# Lasagna

Команды проекта собраны в `Makefile`: `make help` покажет список (стек, frontend, Go-тесты, E2E).

## Запуск демо

Нужен Docker с Compose. Перед первым запуском скопировать `.env.example` в `.env.docker` и проверить параметры окружения.

Из корня репозитория:

```sh
docker compose up -d --build
```

Одна команда поднимает весь проект: базы и моки, backend с миграциями, демо-данные и frontend на `http://localhost:3000` (сборка из `frontend/Dockerfile`, прокси `/api` и `/esign` внутри compose-сети). Одноразовый сервис `demo-seed` дождётся готовности API и создаст 12 демо-аккаунтов с подключёнными компаниями: по шесть сценариев для ООО и ИП. Повторный запуск переиспользует существующие записи. Локальный override (`docker-compose.local.yaml`) убран в корневой файл: порт postgres на хост не публикуется.

Дождаться окончания заполнения:

```sh
docker compose wait demo-seed
docker compose logs demo-seed
```

Код завершения `0` и сообщение `demo seed completed accounts=12` означают готовность демо. Backend доступен на `http://localhost:8080`, frontend — на `http://localhost:3000`.

Логины и API для frontend: [docs/demo-businesses.md](docs/demo-businesses.md). Общий демо-пароль: `DemoCabinet2026!`. После входа актуальные ID компаний получать через `GET /api/v0/companies`: на новой БД UUID отличаются.

Повторить заполнение вручную:

```sh
docker compose run --rm --no-deps demo-seed
```

Актуальный список аккаунтов сохраняется в volume `demo-data`. Из корня репозитория его можно выгрузить:

```sh
docker compose cp demo-seed:/data/demo-businesses.json docs/demo-businesses.json
```

### Финансовый шаг заявки

Перед отправкой клиент получает финансы: `POST /api/v0/applications/{id}/financials/fetch`. Если автоматический источник данных не дал — CSV (`.../financials/csv`, заголовок `inn,month,income,expenses,taxes,owner_withdrawals`, суммы в рублях с двумя знаками после точки; пример — `backend/internal/financials-csv/parser/testdata/financials_example.csv`) или подтверждение, что истории нет (`.../financials/no-history`, отправить без фактов можно только «Старт»). Принятый снимок неизменяем и после отправки не меняется — по нему считает модель. В демо (`FINANCIALS_DEMO_MODES=true`) сбой источника моделируется для заявки: `"demo_mode": "unavailable" | "not_found" | "invalid_response"`. Подробнее — [backend/internal/financials/.md](backend/internal/financials/.md).

### Конвейер проверок

После `submit` заявка проходит `submitted → validation → enrichment → scoring → manual_review` асинхронно, через Kafka: событие пишется в outbox вместе с отправкой и публикуется relay бекенда, этапы читает consumer group `lasagna-pipeline`. Ответ `submit` приходит в `submitted`, обычно за секунду-две заявка доходит до сотрудника. Реестр и БКИ с прекращённой деятельностью или просрочкой от 30 дней дают автоматический отказ (`BUSINESS_TERMINATED`, `CURRENT_OVERDUE_30_PLUS`). Подробнее — [backend/internal/pipeline/.md](backend/internal/pipeline/.md).

Посмотреть события и отставание consumer:

```sh
docker compose exec kafka /opt/kafka/bin/kafka-consumer-groups.sh --bootstrap-server localhost:9092 --describe --group lasagna-pipeline
docker compose exec postgres psql -U postgres -c "select topic, key, published_at from outbox order by id desc limit 10"
```

### Сотрудник банка

Одноразовый сервис `staff-seed` создаёт демо-сотрудника с ролью `underwriter` напрямую в БД: публичная регистрация роль сотрудника не даёт. Логин `underwriter@lasagna.demo`, пароль `DemoUnderwriter2026!`; переопределяются через `STAFF_EMAIL` и `STAFF_PASSWORD`. Повторный запуск дублей не создаёт и пароль не меняет.

```sh
docker compose run --rm --no-deps staff-seed
```

Сотрудник работает с очередью `/api/v0/underwriter/applications`, договоры к предложениям лежат в бакете MinIO `documents`. Подробнее — [backend/internal/underwriting/.md](backend/internal/underwriting/.md).

## Запуск frontend

Frontend собирается прямо в корневом compose (`frontend/Dockerfile`) и стартует вместе со стеком на `http://localhost:3000`. Next проксирует `/api/*` на backend и `/esign/*` на esign-mock, поэтому браузер ходит на один origin; адреса (`NEXT_PUBLIC_API_URL`, `NEXT_PUBLIC_SIGN_API_URL`, `NEXT_PUBLIC_SITE_URL`) задаются в корневом `docker-compose.yaml` — при сборке они попадают в rewrites, при старте — в окружение.

Для локальной разработки можно запустить из исходников (порт 3000 должен быть свободен — сначала остановите docker-frontend: `docker compose stop frontend`):

```sh
cd frontend
npm install
npm run dev
```

Логины: клиент — демо-аккаунты из [docs/demo-businesses.md](docs/demo-businesses.md) (например, `demo.ooo.good@lasagna.test`, пароль `DemoCabinet2026!`), сотрудник — `underwriter@lasagna.demo` / `DemoUnderwriter2026!`.

## E2E-тесты

В каталоге `e2e/` — восемь сценарных тестов на встроенном `node:test`, без внешних зависимостей. Они ходят по HTTP через next-прокси, как это делает браузер, и создают реальные заявки в демо-БД. Полное описание всех проверок и подводных камней — в [TESTING.md](TESTING.md).

Полный порядок запуска:

```sh
# 1. стек целиком: backend + frontend (http://localhost:3000)
cp .env.example .env.docker                        # только при первом запуске
docker compose up -d --build

# 2. тесты (из корня репозитория; frontend уже поднят)
node --test e2e/
```

Если frontend работает на другом порту — укажите базовый адрес: `E2E_BASE=http://localhost:3100 node --test e2e/`. Есть и точечные переопределения: `E2E_API` (путь API) и `E2E_ESIGN` (путь esign-mock). Против backend напрямую, без frontend: `E2E_BASE=http://localhost:8080 node --test e2e/` (для этого адреса esign берётся с `:8084`).

Сценарии:

- `e2e/accounts.test.mjs` — разные демо-аккаунты: `demo.ip.overdue` получает автоматический отказ пайплайна без участия сотрудника; `demo.ooo.good` — сотрудник публикует контрпредложение по экспресс-кредиту, клиент видит изменённые условия и отказывается от него кнопкой (→ cancelled).
- `e2e/concurrent.test.mjs` — длинный сценарий `demo.ooo.good`, где клиент и сотрудник работают с одной заявкой одновременно: параллельные поллины с двух сторон, конфликт ревизий (409 и повтор после обновления), цикл запроса уточнений, решение, подпись через esign-mock (PIN `12345678`), выдачу (issued) и скачивание PDF с обеих сторон.

Тесты необратимы: они добавляют заявки и решения в демо-БД. Для чистого состояния пересоздайте её, удалив том postgres, и заново поднимите стек.
