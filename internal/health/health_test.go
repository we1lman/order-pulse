package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestLivenessIgnoresDependencies(t *testing.T) {
	c := New(time.Second)
	// сломанная зависимость не должна влиять на liveness
	c.Register("postgres", func(context.Context) error {
		return errors.New("connection refused")
	})

	rec := httptest.NewRecorder()
	c.LiveHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("код = %d, ожидали 200", rec.Code)
	}
}

func TestReadinessReflectsChecks(t *testing.T) {
	tests := []struct {
		name     string
		check    CheckFunc
		wantCode int
		wantStat string
	}{
		{
			name:     "зависимость доступна",
			check:    func(context.Context) error { return nil },
			wantCode: http.StatusOK,
			wantStat: StatusUp,
		},
		{
			name:     "зависимость недоступна",
			check:    func(context.Context) error { return errors.New("connection refused") },
			wantCode: http.StatusServiceUnavailable,
			wantStat: StatusDown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := New(time.Second)
			c.Register("postgres", tt.check)

			rec := httptest.NewRecorder()
			c.ReadyHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			if rec.Code != tt.wantCode {
				t.Fatalf("код = %d, ожидали %d", rec.Code, tt.wantCode)
			}

			var report Report
			if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
				t.Fatalf("разбор ответа: %v", err)
			}
			if report.Status != tt.wantStat {
				t.Errorf("status = %q, ожидали %q", report.Status, tt.wantStat)
			}
			if _, ok := report.Checks["postgres"]; !ok {
				t.Error("в отчёте нет проверки postgres")
			}
		})
	}
}

// зависшая проверка не должна вешать пробу, иначе kubelet решит что под мёртв
func TestReadinessRespectsTimeout(t *testing.T) {
	c := New(50 * time.Millisecond)
	c.Register("hung", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	start := time.Now()
	report := c.Ready(context.Background())
	elapsed := time.Since(start)

	if report.Status != StatusDown {
		t.Errorf("status = %q, ожидали %q", report.Status, StatusDown)
	}
	if elapsed > time.Second {
		t.Errorf("проверка заняла %v, таймаут не сработал", elapsed)
	}
}

// после SIGTERM 503 должен появиться сразу, до остановки сервера
func TestReadinessDrainsOnShutdown(t *testing.T) {
	c := New(time.Second)
	c.Register("postgres", func(context.Context) error { return nil })
	c.BeginShutdown()

	rec := httptest.NewRecorder()
	c.ReadyHandler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("код = %d, ожидали 503", rec.Code)
	}

	var report Report
	if err := json.NewDecoder(rec.Body).Decode(&report); err != nil {
		t.Fatalf("разбор ответа: %v", err)
	}
	if report.Status != StatusShuttingDown {
		t.Errorf("status = %q, ожидали %q", report.Status, StatusShuttingDown)
	}
}
