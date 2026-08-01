// общий жизненный цикл процесса для всех бинарников проекта
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// http-сервер, консьюмер kafka, воркер relay - всё укладывается сюда
type Component interface {
	Name() string
	// Start блокируется до штатной остановки или фатальной ошибки
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type Runner struct {
	Log *slog.Logger

	ShutdownTimeout time.Duration
	DrainDelay      time.Duration

	// дёргается сразу по сигналу, обычно переводит /readyz в 503
	BeforeStop func()

	components []Component
}

// порядок важен, гасим в обратном
func (r *Runner) Add(c ...Component) {
	r.components = append(r.components, c...)
}

func (r *Runner) Run(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// буфер на всех, иначе горутины упавших компонентов залипнут на отправке
	errCh := make(chan error, len(r.components))

	var wg sync.WaitGroup
	for _, c := range r.components {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Log.Info("компонент запускается", slog.String("component", c.Name()))
			if err := c.Start(ctx); err != nil {
				errCh <- fmt.Errorf("компонент %s: %w", c.Name(), err)
			}
		}()
	}

	var runErr error
	select {
	case <-ctx.Done():
		r.Log.Info("получен сигнал завершения, останавливаемся")
	case runErr = <-errCh:
		r.Log.Error("компонент упал, гасим процесс", slog.String("error", runErr.Error()))
	}

	// возвращаем дефолтную обработку сигналов, второй ctrl+c добьёт зависший shutdown
	stop()

	if r.BeforeStop != nil {
		r.BeforeStop()
		if r.DrainDelay > 0 {
			r.Log.Info("ждём увода трафика", slog.Duration("drain_delay", r.DrainDelay))
			time.Sleep(r.DrainDelay)
		}
	}

	// от Background, а не от ctx: тот уже отменён сигналом и Shutdown оборвёт живые запросы
	shutdownCtx, cancel := context.WithTimeout(context.Background(), r.ShutdownTimeout)
	defer cancel()

	errs := []error{runErr}
	for i := len(r.components) - 1; i >= 0; i-- {
		c := r.components[i]
		r.Log.Info("останавливаем компонент", slog.String("component", c.Name()))
		if err := c.Stop(shutdownCtx); err != nil {
			errs = append(errs, fmt.Errorf("остановка %s: %w", c.Name(), err))
		}
	}

	wg.Wait()
	r.Log.Info("процесс остановлен")

	return errors.Join(errs...)
}
