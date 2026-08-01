// http-сервер и middleware, роутер стандартный ServeMux
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"

	"github.com/we1lman/order-pulse/internal/config"
)

type Server struct {
	name string
	srv  *http.Server
	log  *slog.Logger
}

func New(name string, cfg config.HTTP, h http.Handler, log *slog.Logger) *Server {
	return &Server{
		name: name,
		log:  log,
		srv: &http.Server{
			Addr:              cfg.Addr,
			Handler:           h,
			ReadHeaderTimeout: cfg.ReadHeaderTimeout,
			ReadTimeout:       cfg.ReadTimeout,
			WriteTimeout:      cfg.WriteTimeout,
			IdleTimeout:       cfg.IdleTimeout,
			// иначе внутренние ошибки net/http полезут в глобальный log мимо нашего формата
			ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn),
			BaseContext: func(net.Listener) context.Context {
				return context.Background()
			},
		},
	}
}

func (s *Server) Name() string { return s.name }

func (s *Server) Start(_ context.Context) error {
	s.log.Info("http-сервер слушает", slog.String("addr", s.srv.Addr))

	if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen and serve: %w", err)
	}
	return nil
}

func (s *Server) Stop(ctx context.Context) error {
	if err := s.srv.Shutdown(ctx); err != nil {
		// не уложились в дедлайн, рвём остатки, иначе процесс не завершится
		if closeErr := s.srv.Close(); closeErr != nil {
			return errors.Join(fmt.Errorf("shutdown: %w", err), fmt.Errorf("close: %w", closeErr))
		}
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
