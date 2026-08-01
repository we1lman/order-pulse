package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/we1lman/order-pulse/internal/app"
	"github.com/we1lman/order-pulse/internal/config"
	"github.com/we1lman/order-pulse/internal/health"
	"github.com/we1lman/order-pulse/internal/httpserver"
	"github.com/we1lman/order-pulse/internal/logging"
)

func main() {
	// вся логика в run(), иначе os.Exit съест defer'ы
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "фатальная ошибка: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("загрузка конфига: %w", err)
	}

	log := logging.New(cfg.App)
	log.Info("запуск сервиса",
		slog.String("addr", cfg.HTTP.Addr),
		slog.String("log_level", cfg.App.LogLevel.String()),
	)

	checker := health.New(cfg.App.ReadinessTimeout)
	// сюда потом checker.Register("postgres", pool.Ping) и остальные зависимости

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", checker.LiveHandler())
	mux.Handle("GET /readyz", checker.ReadyHandler())

	// RequestID первым, чтобы id попал и в лог паники, и в лог доступа
	handler := httpserver.Chain(mux,
		httpserver.RequestID(),
		httpserver.Recover(log),
		httpserver.AccessLog(log, "/healthz", "/readyz"),
	)

	srv := httpserver.New("http", cfg.HTTP, handler, log)

	runner := &app.Runner{
		Log:             log,
		ShutdownTimeout: cfg.App.ShutdownTimeout,
		DrainDelay:      cfg.App.DrainDelay,
		BeforeStop:      checker.BeginShutdown,
	}
	runner.Add(srv)

	return runner.Run(context.Background())
}
