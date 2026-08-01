SHELL := /bin/bash
.DEFAULT_GOAL := help

COMPOSE := docker compose -f deploy/docker-compose.yml
GO      := go
BIN_DIR := bin
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: help
help: ## Показать список целей
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

# ── Инфраструктура ───────────────────────────────────────────────────────────

.PHONY: up
up: ## Поднять локальную платформу
	$(COMPOSE) up -d
	@echo "Ждём готовности сервисов..."
	$(COMPOSE) ps

.PHONY: down
down: ## Остановить платформу (данные сохраняются)
	$(COMPOSE) down

.PHONY: reset
reset: ## Снести платформу вместе с данными
	$(COMPOSE) down -v --remove-orphans

.PHONY: ps
ps: ## Статус контейнеров
	$(COMPOSE) ps

.PHONY: logs
logs: ## Логи платформы (S=имя_сервиса для одного)
	$(COMPOSE) logs -f --tail=100 $(S)

.PHONY: tools
tools: ## Поднять вспомогательные утилиты (kafka-ui на :8081)
	$(COMPOSE) --profile tools up -d

.PHONY: topics
topics: ## Показать топики Kafka
	$(COMPOSE) exec kafka /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:19092 --describe

.PHONY: psql
psql: ## Консоль PostgreSQL
	$(COMPOSE) exec postgres psql -U orderpulse -d orderpulse

.PHONY: redis-cli
redis-cli: ## Консоль Redis
	$(COMPOSE) exec redis redis-cli

.PHONY: ch
ch: ## Консоль ClickHouse
	$(COMPOSE) exec clickhouse clickhouse-client --user orderpulse --password orderpulse --database orderpulse

# ── Разработка ───────────────────────────────────────────────────────────────

.PHONY: run-api
run-api: ## Запустить order-api локально
	$(GO) run ./cmd/order-api

.PHONY: build
build: ## Собрать все бинарники в bin/
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/ ./cmd/...
	@ls -1 $(BIN_DIR)

.PHONY: tidy
tidy: ## Привести go.mod/go.sum в порядок
	$(GO) mod tidy

.PHONY: fmt
fmt: ## Форматирование
	$(GO) fmt ./...

.PHONY: vet
vet: ## go vet
	$(GO) vet ./...

.PHONY: lint
lint: ## golangci-lint (нужен установленный golangci-lint v2)
	golangci-lint run ./...

.PHONY: test
test: ## Юнит-тесты с детектором гонок
	$(GO) test -race -count=1 ./...

.PHONY: cover
cover: ## Тесты с отчётом покрытия
	$(GO) test -race -count=1 -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

.PHONY: check
check: fmt vet test ## Полная проверка перед коммитом

# ── Проверка вручную ─────────────────────────────────────────────────────────

.PHONY: smoke
smoke: ## Дёрнуть пробы запущенного order-api
	@curl -sS -i http://localhost:8080/healthz; echo
	@curl -sS -i http://localhost:8080/readyz; echo
