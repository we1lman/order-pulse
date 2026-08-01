package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	if cfg.App.Env != EnvLocal {
		t.Errorf("Env = %q, ожидали %q", cfg.App.Env, EnvLocal)
	}
	if cfg.App.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, ожидали info", cfg.App.LogLevel)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Errorf("Addr = %q, ожидали :8080", cfg.HTTP.Addr)
	}
	if cfg.HTTP.ReadHeaderTimeout != 5*time.Second {
		t.Errorf("ReadHeaderTimeout = %v, ожидали 5s", cfg.HTTP.ReadHeaderTimeout)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("APP_ENV", "PROD") // регистр не должен иметь значения
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("HTTP_ADDR", ":9999")
	t.Setenv("SHUTDOWN_TIMEOUT", "45s")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	if cfg.App.Env != EnvProd {
		t.Errorf("Env = %q, ожидали %q", cfg.App.Env, EnvProd)
	}
	if cfg.App.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v, ожидали debug", cfg.App.LogLevel)
	}
	if cfg.HTTP.Addr != ":9999" {
		t.Errorf("Addr = %q, ожидали :9999", cfg.HTTP.Addr)
	}
	if cfg.App.ShutdownTimeout != 45*time.Second {
		t.Errorf("ShutdownTimeout = %v, ожидали 45s", cfg.App.ShutdownTimeout)
	}
}

// пустой env из отсутствующего ключа ConfigMap не должен затирать дефолт
func TestEmptyValueFallsBackToDefault(t *testing.T) {
	t.Setenv("HTTP_ADDR", "   ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if cfg.HTTP.Addr != ":8080" {
		t.Errorf("Addr = %q, ожидали значение по умолчанию :8080", cfg.HTTP.Addr)
	}
}

func TestLoadCollectsAllErrors(t *testing.T) {
	t.Setenv("APP_ENV", "staging")          // нет в списке допустимых
	t.Setenv("SHUTDOWN_TIMEOUT", "полчаса") // не парсится
	t.Setenv("HTTP_READ_TIMEOUT", "-5s")    // не положительное
	t.Setenv("LOG_LEVEL", "verbose")        // нет такого уровня

	_, err := Load()
	if err == nil {
		t.Fatal("ожидали ошибку, получили nil")
	}

	msg := err.Error()
	for _, key := range []string{"APP_ENV", "SHUTDOWN_TIMEOUT", "HTTP_READ_TIMEOUT", "LOG_LEVEL"} {
		if !strings.Contains(msg, key) {
			t.Errorf("в ошибке нет упоминания %s:\n%s", key, msg)
		}
	}
}
