package httpserver

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRequestIDGeneratedWhenAbsent(t *testing.T) {
	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), RequestID())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	got := rec.Header().Get(HeaderRequestID)
	if len(got) != 32 {
		t.Fatalf("%s = %q, ожидали 32 hex-символа", HeaderRequestID, got)
	}
}

// id от шлюза переиспользуем, иначе цепочка по логам рвётся на каждом сервисе
func TestRequestIDPreservedFromHeader(t *testing.T) {
	const incoming = "af0e1c22de3b4f5a"

	h := Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), RequestID())

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequestID, incoming)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get(HeaderRequestID); got != incoming {
		t.Errorf("%s = %q, ожидали %q", HeaderRequestID, got, incoming)
	}
}

func TestRecoverReturns500(t *testing.T) {
	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("что-то пошло не так")
	}), Recover(discardLogger()))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("код = %d, ожидали 500", rec.Code)
	}
}

// ErrAbortHandler гасить нельзя, ей net/http рвёт соединение с отвалившимся клиентом
func TestRecoverRepanicsAbortHandler(t *testing.T) {
	defer func() {
		if rec := recover(); rec != http.ErrAbortHandler {
			t.Errorf("паника = %v, ожидали ErrAbortHandler", rec)
		}
	}()

	h := Chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}), Recover(discardLogger()))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

func TestStatusWriterCapturesStatusAndBytes(t *testing.T) {
	var captured *statusWriter

	h := AccessLog(discardLogger())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sw, ok := w.(*statusWriter)
		if !ok {
			t.Fatalf("writer = %T, ожидали *statusWriter", w)
		}
		captured = sw
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, "чайник")
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if captured.status != http.StatusTeapot {
		t.Errorf("status = %d, ожидали 418", captured.status)
	}
	if captured.bytes != int64(len("чайник")) {
		t.Errorf("bytes = %d, ожидали %d", captured.bytes, len("чайник"))
	}
}

func TestAccessLogSkipsProbes(t *testing.T) {
	var sb strings.Builder
	log := slog.New(slog.NewTextHandler(&sb, nil))

	h := AccessLog(log, "/healthz")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if sb.Len() != 0 {
		t.Errorf("проба попала в лог: %s", sb.String())
	}

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/orders", nil))
	if !strings.Contains(sb.String(), "/orders") {
		t.Errorf("обычный запрос не попал в лог: %s", sb.String())
	}
}
