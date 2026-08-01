// пробы живости и готовности
//
// /healthz не трогает зависимости: если повесить туда проверку базы, при её
// падении k8s перезапустит все поды разом и станет только хуже.
// /readyz зависимости проверяет, но провал только убирает под из endpoints.
package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type CheckFunc func(ctx context.Context) error

const (
	StatusUp           = "up"
	StatusDown         = "down"
	StatusShuttingDown = "shutting_down"

	statusCheckPassed   = "ok"
	statusCheckFailed   = "fail"
	defaultCheckTimeout = 2 * time.Second
)

type Result struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	TookMs int64  `json:"took_ms"`
}

type Report struct {
	Status string            `json:"status"`
	Checks map[string]Result `json:"checks,omitempty"`
}

type Checker struct {
	mu     sync.RWMutex
	checks map[string]CheckFunc

	timeout  time.Duration
	draining atomic.Bool
}

func New(timeout time.Duration) *Checker {
	if timeout <= 0 {
		timeout = defaultCheckTimeout
	}
	return &Checker{
		checks:  make(map[string]CheckFunc),
		timeout: timeout,
	}
}

func (c *Checker) Register(name string, fn CheckFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checks[name] = fn
}

// вызывается по SIGTERM, чтобы балансировщик увёл трафик до остановки сервера
func (c *Checker) BeginShutdown() {
	c.draining.Store(true)
}

func (c *Checker) Ready(ctx context.Context) Report {
	c.mu.RLock()
	checks := maps.Clone(c.checks)
	c.mu.RUnlock()

	report := Report{
		Status: StatusUp,
		Checks: make(map[string]Result, len(checks)),
	}
	if len(checks) == 0 {
		return report
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	var (
		mu sync.Mutex
		wg sync.WaitGroup
	)
	for name, fn := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()

			start := time.Now()
			err := fn(ctx)
			res := Result{
				Status: statusCheckPassed,
				TookMs: time.Since(start).Milliseconds(),
			}
			if err != nil {
				res.Status = statusCheckFailed
				res.Error = err.Error()
			}

			mu.Lock()
			defer mu.Unlock()
			report.Checks[name] = res
			if err != nil {
				report.Status = StatusDown
			}
		}()
	}
	wg.Wait()

	return report
}

func (c *Checker) LiveHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(r.Context(), w, http.StatusOK, Report{Status: StatusUp})
	})
}

func (c *Checker) ReadyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c.draining.Load() {
			writeJSON(r.Context(), w, http.StatusServiceUnavailable, Report{Status: StatusShuttingDown})
			return
		}

		report := c.Ready(r.Context())
		code := http.StatusOK
		if report.Status != StatusUp {
			code = http.StatusServiceUnavailable
		}
		writeJSON(r.Context(), w, code, report)
	})
}

func writeJSON(ctx context.Context, w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		// заголовки уже ушли, ответ не починить
		slog.ErrorContext(ctx, "не смогли записать ответ пробы", slog.String("error", err.Error()))
	}
}
