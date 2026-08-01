package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"
)

// пишет порядок вызовов в общий журнал
type fakeComponent struct {
	name     string
	journal  *journal
	startErr error
}

func (c *fakeComponent) Name() string { return c.name }

func (c *fakeComponent) Start(ctx context.Context) error {
	c.journal.add("start:" + c.name)
	if c.startErr != nil {
		return c.startErr
	}
	<-ctx.Done()
	return nil
}

func (c *fakeComponent) Stop(context.Context) error {
	c.journal.add("stop:" + c.name)
	return nil
}

type journal struct {
	mu      sync.Mutex
	entries []string
}

func (j *journal) add(e string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.entries = append(j.entries, e)
}

func (j *journal) snapshot() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return slices.Clone(j.entries)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// сначала гасим приём трафика, потом то что его обслуживает
func TestRunnerStopsInReverseOrder(t *testing.T) {
	j := &journal{}
	r := &Runner{Log: discardLogger(), ShutdownTimeout: time.Second}
	r.Add(
		&fakeComponent{name: "db", journal: j},
		&fakeComponent{name: "http", journal: j},
	)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	if err := r.Run(ctx); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	got := j.snapshot()
	stops := make([]string, 0, 2)
	for _, e := range got {
		if len(e) > 5 && e[:5] == "stop:" {
			stops = append(stops, e)
		}
	}
	want := []string{"stop:http", "stop:db"}
	if !slices.Equal(stops, want) {
		t.Errorf("порядок остановки = %v, ожидали %v", stops, want)
	}
}

func TestRunnerStopsOnComponentFailure(t *testing.T) {
	j := &journal{}
	boom := errors.New("не удалось занять порт")

	r := &Runner{Log: discardLogger(), ShutdownTimeout: time.Second}
	r.Add(
		&fakeComponent{name: "worker", journal: j},
		&fakeComponent{name: "http", journal: j, startErr: boom},
	)

	err := r.Run(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("ошибка = %v, ожидали обёртку над %v", err, boom)
	}

	if !slices.Contains(j.snapshot(), "stop:worker") {
		t.Error("исправный компонент не был остановлен")
	}
}

func TestRunnerCallsBeforeStop(t *testing.T) {
	j := &journal{}
	r := &Runner{
		Log:             discardLogger(),
		ShutdownTimeout: time.Second,
		BeforeStop:      func() { j.add("before_stop") },
	}
	r.Add(&fakeComponent{name: "http", journal: j})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	if err := r.Run(ctx); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	got := j.snapshot()
	beforeIdx := slices.Index(got, "before_stop")
	stopIdx := slices.Index(got, "stop:http")
	if beforeIdx < 0 || stopIdx < 0 {
		t.Fatalf("журнал неполный: %v", got)
	}
	if beforeIdx > stopIdx {
		t.Errorf("BeforeStop вызван после остановки компонента: %v", got)
	}
}
