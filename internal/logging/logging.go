// логгер на log/slog, атрибуты подтягиваются из контекста
package logging

import (
	"context"
	"log/slog"
	"os"

	"github.com/we1lman/order-pulse/internal/config"
)

type ctxKey struct{}

// кладём request_id/order_id один раз, дальше они сами попадут в каждую запись
func WithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	if len(attrs) == 0 {
		return ctx
	}
	existing, _ := ctx.Value(ctxKey{}).([]slog.Attr)

	// копируем, срез из контекста могут читать другие горутины
	merged := make([]slog.Attr, 0, len(existing)+len(attrs))
	merged = append(merged, existing...)
	merged = append(merged, attrs...)

	return context.WithValue(ctx, ctxKey{}, merged)
}

func AttrsFrom(ctx context.Context) []slog.Attr {
	attrs, _ := ctx.Value(ctxKey{}).([]slog.Attr)
	return attrs
}

type contextHandler struct {
	slog.Handler
}

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if attrs := AttrsFrom(ctx); len(attrs) > 0 {
		r.AddAttrs(attrs...)
	}
	return h.Handler.Handle(ctx, r)
}

// без переопределения logger.With() вернёт голый хендлер и обёртка потеряется
func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{Handler: h.Handler.WithGroup(name)}
}

func New(cfg config.App) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: cfg.LogLevel,
		// source считается через стек, на горячем пути дорого
		AddSource: cfg.LogLevel <= slog.LevelDebug,
	}

	var h slog.Handler
	switch cfg.LogFormat {
	case "text":
		h = slog.NewTextHandler(os.Stdout, opts)
	default:
		h = slog.NewJSONHandler(os.Stdout, opts)
	}

	return slog.New(contextHandler{Handler: h}).With(
		slog.String("service", cfg.Name),
		slog.String("env", cfg.Env),
		slog.String("version", cfg.Version),
	)
}
