COMPOSE := docker compose
PROJECT := $(notdir $(CURDIR))
E2E_BASE ?= http://localhost:3000

.DEFAULT_GOAL := help

.PHONY: help up ready down ps logs db seed seed-staff db-reset events lag \
        install dev build start lint typecheck test-backend test e2e check

help: ## Список целей
	@grep -E '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-14s %s\n", $$1, $$2}'

up: ## Поднять всё: backend, моки, базы, миграции, сиды и frontend (:3000)
	@test -f .env.docker || cp .env.example .env.docker
	$(COMPOSE) up -d --build

ready: ## Дождаться миграций и демо-данных
	@for i in $$(seq 1 120); do \
		st=$$($(COMPOSE) ps --all --format '{{.Status}}' demo-seed 2>/dev/null); \
		case "$$st" in \
			"Exited (0)"*) $(COMPOSE) logs --tail 5 demo-seed; exit 0 ;; \
			"Exited "*|ERROR*) echo "demo-seed: $$st"; exit 1 ;; \
		esac; \
		sleep 2; \
	done; \
	echo "demo-seed не завершился за 4 минуты"; exit 1

down: ## Остановить стек
	$(COMPOSE) down

ps: ## Статус контейнеров
	$(COMPOSE) ps

logs: ## Логи backend (Ctrl+C — выход)
	$(COMPOSE) logs -f backend

db: ## Консоль postgres
	$(COMPOSE) exec postgres psql -U postgres

seed: ## Пересоздать демо-аккаунты
	$(COMPOSE) run --rm --no-deps demo-seed

seed-staff: ## Пересоздать сотрудника банка
	$(COMPOSE) run --rm --no-deps staff-seed

db-reset: ## Удалить БД и поднять стек заново (нужен CONFIRM=1)
	@test "$(CONFIRM)" = "1" || { echo "База данных будет удалена. Подтвердите: make db-reset CONFIRM=1"; exit 1; }
	$(COMPOSE) down
	docker volume rm $(PROJECT)_postgres-data
	$(COMPOSE) up -d --build

events: ## Последние события outbox в postgres
	$(COMPOSE) exec postgres psql -U postgres -c "select topic, key, published_at from outbox order by id desc limit 10"

lag: ## Отставание consumer'а pipeline в Kafka
	$(COMPOSE) exec kafka /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group lasagna-pipeline

install: ## Установить зависимости frontend
	cd frontend && npm install

dev: ## Запустить frontend из исходников (npm dev; остановите docker-frontend на :3000)
	cd frontend && npm run dev

build: ## Собрать frontend
	cd frontend && npm run build

start: ## Запустить frontend из сборки
	cd frontend && npm run start

lint: ## Проверка стилей frontend
	cd frontend && npm run lint

typecheck: ## Проверка типов frontend
	cd frontend && npm run typecheck

test-backend: ## Go-тесты бэкенда в docker
	docker run --rm -v $(CURDIR)/backend:/src -w /src golang:1.27.1-alpine go test ./...

test: e2e ## Алиас для e2e

e2e: ## E2E-тесты (нужны стек и frontend; адрес — E2E_BASE)
ifndef E2E_API
	@code=$$(curl -s -o /dev/null -w '%{http_code}' --connect-timeout 3 $(E2E_BASE)/login || true); \
	if [ -z "$$code" ] || [ "$$code" = "000" ]; then \
		echo "Frontend недоступен на $(E2E_BASE) — запустите make dev или задайте E2E_BASE=..."; \
		exit 1; \
	fi
endif
	node --test e2e/

check: typecheck lint test-backend e2e ## Все проверки: типы, стили, Go-тесты, e2e
