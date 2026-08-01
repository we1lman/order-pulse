package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"time"

	"github.com/we1lman/order-pulse/internal/logging"
)

const HeaderRequestID = "X-Request-Id"

type Middleware func(http.Handler) http.Handler

// с конца, чтобы первый в списке выполнялся первым
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// если шлюз уже проставил, переиспользуем, иначе цепочка по логам порвётся
			id := r.Header.Get(HeaderRequestID)
			if id == "" {
				id = newRequestID()
			}
			w.Header().Set(HeaderRequestID, id)

			ctx := logging.WithAttrs(r.Context(), slog.String("request_id", id))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b[:])
}

// паника в горутине обработчика роняет весь процесс, тут recover обязателен
func Recover(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				rec := recover()
				if rec == nil {
					return
				}
				// служебная паника net/http, её глотать нельзя
				if rec == http.ErrAbortHandler { //nolint:errorlint // сравнение по значению, как в net/http
					panic(rec)
				}

				log.ErrorContext(r.Context(), "паника в обработчике",
					slog.Any("panic", rec),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.String("stack", string(debug.Stack())),
				)
				w.WriteHeader(http.StatusInternalServerError)
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// skipPaths нужен для проб, иначе kubelet забьёт логи своими дёрганьями
func AccessLog(log *slog.Logger, skipPaths ...string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if slices.Contains(skipPaths, r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(sw, r)

			level := slog.LevelInfo
			switch {
			case sw.status >= http.StatusInternalServerError:
				level = slog.LevelError
			case sw.status >= http.StatusBadRequest:
				level = slog.LevelWarn
			}

			log.LogAttrs(r.Context(), level, "запрос обработан",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", sw.status),
				slog.Int64("bytes", sw.bytes),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("user_agent", r.UserAgent()),
			)
		})
	}
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// без Unwrap обёртка отрежет Flush и Hijack, сломается стриминг и вебсокеты
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
