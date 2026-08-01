// конфиг из переменных окружения, всё валидируется на старте
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"
)

const (
	EnvLocal = "local"
	EnvDev   = "dev"
	EnvProd  = "prod"
)

type Config struct {
	App  App
	HTTP HTTP
}

type App struct {
	Name    string
	Env     string
	Version string

	LogLevel  slog.Level
	LogFormat string

	// должен быть меньше terminationGracePeriodSeconds, иначе прилетит SIGKILL
	ShutdownTimeout time.Duration

	// пауза между 503 на /readyz и остановкой сервера, ждём пока kube-proxy обновит правила
	DrainDelay time.Duration

	ReadinessTimeout time.Duration
}

type HTTP struct {
	Addr string

	// без него сервер уязвим к slowloris
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

func Load() (Config, error) {
	l := &loader{}

	cfg := Config{
		App: App{
			Name:             l.str("APP_NAME", "order-api"),
			Env:              l.enum("APP_ENV", EnvLocal, EnvLocal, EnvDev, EnvProd),
			Version:          l.str("APP_VERSION", "dev"),
			LogLevel:         l.level("LOG_LEVEL", slog.LevelInfo),
			LogFormat:        l.enum("LOG_FORMAT", "json", "json", "text"),
			ShutdownTimeout:  l.dur("SHUTDOWN_TIMEOUT", 15*time.Second),
			DrainDelay:       l.dur("DRAIN_DELAY", 5*time.Second),
			ReadinessTimeout: l.dur("READINESS_TIMEOUT", 2*time.Second),
		},
		HTTP: HTTP{
			Addr:              l.str("HTTP_ADDR", ":8080"),
			ReadHeaderTimeout: l.dur("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			ReadTimeout:       l.dur("HTTP_READ_TIMEOUT", 15*time.Second),
			WriteTimeout:      l.dur("HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:       l.dur("HTTP_IDLE_TIMEOUT", 60*time.Second),
		},
	}

	if err := l.err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// копит ошибки, чтобы отдать все разом а не по одной за перезапуск
type loader struct {
	errs []error
}

func (l *loader) err() error {
	return errors.Join(l.errs...)
}

func (l *loader) fail(key, raw string, err error) {
	l.errs = append(l.errs, fmt.Errorf("config: %s=%q: %w", key, raw, err))
}

// пустая строка считается "не задано", из пустого ключа ConfigMap прилетает именно она
func (l *loader) str(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return def
}

func (l *loader) enum(key, def string, allowed ...string) string {
	v := strings.ToLower(l.str(key, def))
	if !slices.Contains(allowed, v) {
		l.fail(key, v, fmt.Errorf("допустимо: %s", strings.Join(allowed, ", ")))
		return def
	}
	return v
}

func (l *loader) dur(key string, def time.Duration) time.Duration {
	raw := l.str(key, "")
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		l.fail(key, raw, err)
		return def
	}
	if d <= 0 {
		l.fail(key, raw, errors.New("должно быть больше нуля"))
		return def
	}
	return d
}

func (l *loader) level(key string, def slog.Level) slog.Level {
	raw := l.str(key, "")
	if raw == "" {
		return def
	}
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(raw)); err != nil {
		l.fail(key, raw, err)
		return def
	}
	return lvl
}
