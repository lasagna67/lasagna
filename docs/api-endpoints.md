# HTTP API бэкенда

Документация всех 49 HTTP-операций, описанных в Swagger бэкенда. Источник спецификации: [swagger.json](./swagger.json) и [swagger.yaml](./swagger.yaml).

**Base path:** `/api/v0`

Для защищённых операций передавайте access token в заголовке `Authorization` со схемой `Bearer`. Токен выдаёт `POST /api/v0/auth/login`. Поля запросов и ответов раскрыты в схемах Swagger; здесь перечислены назначение ручек, параметры и HTTP-статусы.

## applications

### `GET /api/v0/applications` — Заявки компании

Заявки компании, где пользователь — представитель, новые сверху; иначе 404. После отправки статус меняется по этапам конвейера асинхронно — список и карточка показывают текущий.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `query company_id` (обязательный, `string (uuid)`) — Компания
- `query status` (необязательный, `string`) — Статус
- `query limit` (необязательный, `integer`) — До 100, по умолчанию 20
- `query cursor` (необязательный, `string`) — next_cursor предыдущей страницы

**Ответы:**
- `200` — OK (`#/definitions/intakedto.ApplicationPageResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `401` — Unauthorized (`#/definitions/applicationdto.ErrorResponse`)
- `403` — не клиент (`#/definitions/swagger.HTTPError`)
- `404` — NOT_FOUND: нет компании или пользователь не её участник (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/applications` — Новая заявка

Черновик от имени компании, в которой пользователь — представитель. Повтор с тем же Idempotency-Key и телом возвращает ту же заявку, с другим телом — 409 IDEMPOTENCY_CONFLICT.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `header Idempotency-Key` (обязательный, `string`) — Ключ повтора, до 255 символов
- `body request` (обязательный, `#/definitions/intakedto.CreateApplicationRequest`) — Компания-заёмщик

**Ответы:**
- `201` — Created (`#/definitions/applicationdto.ApplicationResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `403` — не клиент (`#/definitions/swagger.HTTPError`)
- `404` — NOT_FOUND: нет компании или пользователь не её участник (`#/definitions/applicationdto.ErrorResponse`)
- `409` — IDEMPOTENCY_CONFLICT (`#/definitions/applicationdto.ErrorResponse`)
- `422` — VALIDATION_FAILED (`#/definitions/applicationdto.ErrorResponse`)

### `GET /api/v0/applications/{id}` — Заявка

Заявка своей компании в любом статусе, с залогом, полями проекта и планом. После отправки по ней видно, на каком этапе конвейера заявка.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка

**Ответы:**
- `200` — OK (`#/definitions/applicationdto.ApplicationResponse`)
- `401` — Unauthorized (`#/definitions/applicationdto.ErrorResponse`)
- `403` — не клиент (`#/definitions/swagger.HTTPError`)
- `404` — NOT_FOUND: нет заявки или она чужая (`#/definitions/applicationdto.ErrorResponse`)

### `PATCH /api/v0/applications/{id}` — Изменить заявку

В draft — любые поля запроса и залог, в info_requested — только из editable_fields, иначе 409 INVALID_STATE; поле вне editable_fields — 422 NOT_EDITABLE. Непереданное поле не меняется, null очищает значение. need_period и collateral заменяются целиком; collateral — только при collateral_mode = pledged. product_details — поля «Старта», cash_plan — денежный план: заменяются целиком, null удаляет. План в черновике — только у «Старта» (kind = project); план ликвидности (liquidity) — по запросу банка, у овердрафта плана нет. Правка содержимого снимает подтверждение; подтверждение — отдельной правкой {"cash_plan": {"confirmed": true}}, когда прогноз начинается со следующего месяца и покрывает весь срок кредита, иначе 422 FORECAST_CONFIRMATION_REQUIRED. data_revision растёт, только если изменились данные. Полноту, бюджет «Старта» и условия продукта проверяет submit.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `body request` (обязательный, `#/definitions/intakedto.PatchApplicationRequest`) — Изменения

**Ответы:**
- `200` — OK (`#/definitions/applicationdto.ApplicationResponse`)
- `400` — INVALID_REQUEST: тело, неизвестное поле, нет изменений (`#/definitions/applicationdto.ErrorResponse`)
- `401` — Unauthorized (`#/definitions/applicationdto.ErrorResponse`)
- `403` — не клиент (`#/definitions/swagger.HTTPError`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — REVISION_CONFLICT, INVALID_STATE (`#/definitions/applicationdto.ErrorResponse`)
- `422` — VALIDATION_FAILED (`#/definitions/applicationdto.ErrorResponse`)

### `GET /api/v0/applications/{id}/contract` — Договор к предложению

Метаданные; файл — GET /documents/{id}/content с id договора.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка

**Ответы:**
- `200` — null — договора нет (`#/definitions/offerdto.ContractResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/applications/{id}/decline` — Отказаться от предложения

Переводит заявку из approved или counter_offer в cancelled. Повтор с тем же Idempotency-Key и expected_revision возвращает тот же результат.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `header Idempotency-Key` (обязательный, `string`) — Ключ повтора, до 255 символов
- `body request` (обязательный, `#/definitions/offerdto.DeclineRequest`) — Ожидаемая revision

**Ответы:**
- `200` — OK (`#/definitions/applicationdto.ApplicationResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `401` — UNAUTHENTICATED (`#/definitions/applicationdto.ErrorResponse`)
- `403` — не клиент (`#/definitions/swagger.HTTPError`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — REVISION_CONFLICT, INVALID_STATE, IDEMPOTENCY_CONFLICT (`#/definitions/applicationdto.ErrorResponse`)
- `500` — INTERNAL_ERROR (`#/definitions/applicationdto.ErrorResponse`)

### `GET /api/v0/applications/{id}/info-requests` — Запросы банка

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка

**Ответы:**
- `200` — OK (`array`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `404` — NOT_FOUND: нет заявки или она чужая (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/applications/{id}/info-requests/{rid}/answer` — Ответить на запрос банка

Поля из editable_fields правятся через PATCH заявки; после ответов на все запросы — resubmit.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `path rid` (обязательный, `string (uuid)`) — Запрос
- `body request` (обязательный, `#/definitions/underwritingdto.AnswerInfoRequest`) — Ответ

**Ответы:**
- `200` — OK (`#/definitions/applicationdto.ApplicationResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — REVISION_CONFLICT, INVALID_STATE (`#/definitions/applicationdto.ErrorResponse`)
- `422` — VALIDATION_FAILED (`#/definitions/applicationdto.ErrorResponse`)

### `GET /api/v0/applications/{id}/offer` — Текущее предложение

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка

**Ответы:**
- `200` — null — предложения нет (`#/definitions/offerdto.OfferResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/applications/{id}/resubmit` — Отправить повторно

info_requested → submitted после ответов на все запросы банка; запрос проверяется как при отправке. Новый запуск конвейера: этапы идут асинхронно, как после submit. Если банк открыл cash_plan, план должен быть подтверждён на срок кредита. Повтор с тем же Idempotency-Key и телом не создаёт второй отправки.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `header Idempotency-Key` (обязательный, `string`) — Ключ повтора, до 255 символов
- `body request` (обязательный, `#/definitions/intakedto.RevisionRequest`) — Ожидаемая revision

**Ответы:**
- `200` — OK (`#/definitions/applicationdto.ApplicationResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `401` — Unauthorized (`#/definitions/applicationdto.ErrorResponse`)
- `403` — не клиент (`#/definitions/swagger.HTTPError`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — REVISION_CONFLICT, INVALID_STATE, IDEMPOTENCY_CONFLICT (`#/definitions/applicationdto.ErrorResponse`)
- `422` — VALIDATION_FAILED (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/applications/{id}/sign` — Проверить криптографическую подпись договора

Backend проверяет подпись через esign-mock; PIN и сертификат от клиента не принимает. Атомарно сохраняет артефакт, accepted, signing и задачу выдачи pending. Повтор с тем же ключом и телом не создаёт новую операцию.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `header Idempotency-Key` (обязательный, `string`) — Ключ повтора, до 255 символов
- `body request` (обязательный, `#/definitions/signdto.SignRequest`) — Контекст, подпись и согласие с условиями

**Ответы:**
- `200` — OK (`#/definitions/signdto.Signature`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `401` — Требуется авторизация (`#/definitions/swagger.HTTPError`)
- `403` — FORBIDDEN (`#/definitions/applicationdto.ErrorResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — REVISION_CONFLICT, OFFER_CHANGED, SIGNING_CONTEXT_EXPIRED, IDEMPOTENCY_CONFLICT, INVALID_STATE (`#/definitions/applicationdto.ErrorResponse`)
- `422` — INVALID_SIGNATURE (`#/definitions/applicationdto.ErrorResponse`)
- `503` — SOURCE_RECHECK_REQUIRED или SERVICE_UNAVAILABLE (`#/definitions/applicationdto.ErrorResponse`)

### `GET /api/v0/applications/{id}/signature` — Скачать артефакт подписи

JSON: подписанные данные, подпись, проверенный сертификат и хеш исходного PDF.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка

**Ответы:**
- `200` — OK (`#/definitions/signdto.Signature`)
- `401` — Требуется авторизация (`#/definitions/swagger.HTTPError`)
- `403` — FORBIDDEN (`#/definitions/applicationdto.ErrorResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/applications/{id}/signing-context` — Подготовить контекст подписи договора

TTL до 5 минут, с учётом срока проверок. Контекст связан с пользователем, revision, Offer, Contract и SHA-256 исходного PDF. При неподтверждённом P14 — 503.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `body request` (обязательный, `#/definitions/signdto.PrepareRequest`) — Предложение, договор и версия заявки

**Ответы:**
- `200` — OK (`#/definitions/signdto.Context`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `401` — Требуется авторизация (`#/definitions/swagger.HTTPError`)
- `403` — FORBIDDEN (`#/definitions/applicationdto.ErrorResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — REVISION_CONFLICT, OFFER_CHANGED, INVALID_STATE (`#/definitions/applicationdto.ErrorResponse`)
- `503` — SOURCE_RECHECK_REQUIRED или SERVICE_UNAVAILABLE (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/applications/{id}/submit` — Отправить заявку

draft → submitted с новым запуском конвейера. Ответ — в submitted: этапы validation → enrichment → scoring → manual_review идут асинхронно, текущий статус — GET /applications/{id}. Неполный запрос или нарушение условий продукта — 422 с полями, заявка остаётся draft. У «Старта» обязательны полные product_details (кредит + собственные средства = стоимость, собственных не меньше 30%) и подтверждённый cash_plan на срок кредита. Повтор с тем же Idempotency-Key и телом не создаёт второй отправки.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `header Idempotency-Key` (обязательный, `string`) — Ключ повтора, до 255 символов
- `body request` (обязательный, `#/definitions/intakedto.RevisionRequest`) — Ожидаемая revision

**Ответы:**
- `200` — OK (`#/definitions/applicationdto.ApplicationResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `401` — Unauthorized (`#/definitions/applicationdto.ErrorResponse`)
- `403` — не клиент (`#/definitions/swagger.HTTPError`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — REVISION_CONFLICT, INVALID_STATE, IDEMPOTENCY_CONFLICT (`#/definitions/applicationdto.ErrorResponse`)
- `422` — VALIDATION_FAILED (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/underwriter/applications/{id}/collateral-review` — Записать банковскую проверку залога

Только назначенный активный underwriter/admin в manual_review или info_requested. Неизменяемое заключение сохраняет версию заявки, версию и SHA-256 залога. Для accepted нужны положительная оценка и внутренний comment. Актуальность — 30 суток включительно. Изменение суммы кредита только пересчитывает покрытие 70%.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — ID заявки
- `body request` (обязательный, `#/definitions/collateraldomain.ReviewCollateral`) — Заключение

**Ответы:**
- `200` — OK (`#/definitions/collateraldomain.CollateralApplication`)
- `400` — Bad Request (`#/definitions/swagger.HTTPError`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `403` — Forbidden (`#/definitions/swagger.HTTPError`)
- `404` — Not Found (`#/definitions/swagger.HTTPError`)
- `409` — Conflict (`#/definitions/swagger.HTTPError`)
- `500` — Internal Server Error (`#/definitions/swagger.HTTPError`)

## financials

### `GET /api/v0/applications/{id}/financials` — Финансовый шаг заявки

Источник, принятый снимок текущего цикла, пригодность на бизнес-дату и доступность резервной ветки (CSV, отсутствие истории).

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка

**Ответы:**
- `200` — OK (`#/definitions/applicationdto.FinancialStateResponse`)
- `401` — Unauthorized (`#/definitions/applicationdto.ErrorResponse`)
- `403` — не клиент (`#/definitions/swagger.HTTPError`)
- `404` — NOT_FOUND: нет заявки или она чужая (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/applications/{id}/financials/csv` — Загрузить финансы из CSV

Резервная ветка: только после not_found, unavailable или invalid автоматического источника и без принятого снимка текущего цикла, иначе 409 CSV_NOT_ALLOWED. UTF-8 (BOM допустим), разделитель запятая или точка с запятой; колонки inn,month,income,expenses,taxes, owner_withdrawals и необязательная free_cash; суммы в рублях с точкой, до двух знаков; завершённые месяцы не раньше регистрации, до 120 строк, файл до 5 МиБ. Ошибка любой строки — 422 INVALID_CSV со всеми найденными ошибками (file.rows[N].колонка), файл не принимается. Принятый CSV оставляет заявку на ручном рассмотрении.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `multipart/form-data`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `header Idempotency-Key` (обязательный, `string`) — Ключ повтора, до 255 символов
- `formData expected_revision` (обязательный, `integer`) — Ожидаемая revision
- `formData file` (обязательный, `file`) — CSV

**Ответы:**
- `200` — OK (`#/definitions/applicationdto.FinancialStateResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — CSV_NOT_ALLOWED, REVISION_CONFLICT, INVALID_STATE, IDEMPOTENCY_CONFLICT (`#/definitions/applicationdto.ErrorResponse`)
- `413` — запрос больше 5 МиБ (`#/definitions/applicationdto.ErrorResponse`)
- `415` — ожидается multipart/form-data (`#/definitions/applicationdto.ErrorResponse`)
- `422` — INVALID_CSV (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/applications/{id}/financials/fetch` — Получить финансы автоматически

Одна попытка источника не дольше таймаута (5 с). Успех — новый принятый снимок, data_revision растёт при изменении данных. not_found, unavailable, invalid открывают резервную ветку: CSV или отсутствие истории; прежний снимок цикла при неуспехе остаётся. В draft или в info_requested, если банк открыл financials. demo_mode — только в демо.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `header Idempotency-Key` (обязательный, `string`) — Ключ повтора, до 255 символов
- `body request` (обязательный, `#/definitions/financialsdto.FetchRequest`) — Ожидаемая revision

**Ответы:**
- `200` — OK (`#/definitions/applicationdto.FinancialStateResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — REVISION_CONFLICT, INVALID_STATE, IDEMPOTENCY_CONFLICT (`#/definitions/applicationdto.ErrorResponse`)
- `422` — VALIDATION_FAILED: financials NOT_EDITABLE (`#/definitions/applicationdto.ErrorResponse`)

### `GET /api/v0/applications/{id}/financials/history` — История финансовых снимков заявки

Неизменяемые принятые снимки всех финансовых циклов, новые сверху.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `query limit` (необязательный, `integer`) — До 100, по умолчанию 20
- `query cursor` (необязательный, `string`) — next_cursor предыдущей страницы

**Ответы:**
- `200` — OK (`#/definitions/applicationdto.FinancialSnapshotPageResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/applications/{id}/financials/no-history` — Подтвердить отсутствие финансовой истории

Только в резервной ветке без принятых данных, иначе 409 INVALID_STATE; confirmed=true обязательно. Отправить заявку без фактов можно только по «Старту». Принятый позже CSV снимает отметку.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `header Idempotency-Key` (обязательный, `string`) — Ключ повтора, до 255 символов
- `body request` (обязательный, `#/definitions/financialsdto.NoHistoryRequest`) — Подтверждение

**Ответы:**
- `200` — OK (`#/definitions/applicationdto.FinancialStateResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — REVISION_CONFLICT, INVALID_STATE, IDEMPOTENCY_CONFLICT (`#/definitions/applicationdto.ErrorResponse`)

### `GET /api/v0/applications/{id}/financials/template` — Шаблон CSV

Колонки и месяцы окна расчёта с ИНН компании, суммы пустые: незаполненный шаблон не принимается.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка

**Ответы:**
- `200` — CSV (`string`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)

### `GET /api/v0/companies/{id}/financials` — Финансовый профиль компании

Последний принятый снимок по заявкам компании с источником, датой и периодом; нет — null.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Компания

**Ответы:**
- `200` — OK (`#/definitions/applicationdto.FinancialSnapshotResponse`)
- `404` — NOT_FOUND: нет компании или пользователь не её участник (`#/definitions/applicationdto.ErrorResponse`)

## auth

### `POST /api/v0/auth/login` — Вход

**Авторизация:** не требуется.
**Content-Type:** `application/json`.

**Параметры:**
- `body request` (обязательный, `#/definitions/dto.LoginRequest`) — Email и пароль

**Ответы:**
- `200` — OK (`#/definitions/dto.AuthResponse`)
- `400` — Bad Request (`#/definitions/swagger.HTTPError`)
- `401` — неверный email или пароль (`#/definitions/swagger.HTTPError`)
- `403` — пользователь заблокирован (`#/definitions/swagger.HTTPError`)
- `500` — Internal Server Error (`#/definitions/swagger.HTTPError`)

### `POST /api/v0/auth/logout` — Выход

**Авторизация:** требуется Bearer-токен.

**Ответы:**
- `204` — No Content
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `500` — Internal Server Error (`#/definitions/swagger.HTTPError`)

### `GET /api/v0/auth/me` — Текущий пользователь

Возвращает id и роль владельца токена.

**Авторизация:** требуется Bearer-токен.

**Ответы:**
- `200` — OK (`#/definitions/dto.UserResponse`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)

## companies

### `GET /api/v0/companies` — Мои компании

**Авторизация:** требуется Bearer-токен.

**Ответы:**
- `200` — OK (`array`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)

### `POST /api/v0/companies` — Подключение компании

Шаг 2 из 2. Проверяет подпись заявления и сверяет владельца сертификата с руководителем по ЕГРЮЛ или с ИНН ИП по ЕГРИП. Пользователь становится ceo (ЮЛ) или owner (ИП) с правом подписи. Заявление тратится при любом исходе.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `body request` (обязательный, `#/definitions/companydto.ConnectRequest`) — Подписанное заявление

**Ответы:**
- `201` — Created (`#/definitions/companydto.CompanyResponse`)
- `400` — Bad Request (`#/definitions/swagger.HTTPError`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `403` — сертификат не руководителя (`#/definitions/swagger.HTTPError`)
- `410` — заявление истекло или уже использовано (`#/definitions/swagger.HTTPError`)
- `422` — подпись не прошла проверку (`#/definitions/swagger.HTTPError`)
- `503` — реестр или УЦ недоступны (`#/definitions/swagger.HTTPError`)

### `GET /api/v0/companies/{id}` — Компания

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — id компании

**Ответы:**
- `200` — OK (`#/definitions/companydto.CompanyResponse`)
- `400` — Bad Request (`#/definitions/swagger.HTTPError`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `404` — нет компании или пользователь не её участник (`#/definitions/swagger.HTTPError`)

### `POST /api/v0/companies/{id}/financials` — Загрузка финансовой отчётности из CSV

Только для client, состоящего в компании. Передайте один CSV в поле file. Максимальный размер всего multipart-запроса — 2 МиБ. Заголовок: inn,month,income,expenses,taxes,owner_withdrawals. Все суммы — неотрицательные целые копейки, месяц — YYYY-MM. ИНН каждой строки должен совпадать с компанией, месяцы не должны повторяться. free_cash = income - expenses - taxes - owner_withdrawals. Файл проверяется полностью до записи. Загрузка заменяет сохранённый финансовый отчёт компании; реестр и БКИ сохраняются. Ответ содержит сохранённый отчёт, месяцы отсортированы. Повторная загрузка не создаёт отдельную строку overview.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `multipart/form-data`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — ID компании
- `formData file` (обязательный, `file`) — CSV в UTF-8 с разделителем запятая; суммы в копейках

**Ответы:**
- `200` — OK (`#/definitions/companydomain.FinancialReport`)
- `400` — Некорректный CSV, ИНН, ID компании или отсутствие файла (`#/definitions/swagger.HTTPError`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `403` — Forbidden (`#/definitions/swagger.HTTPError`)
- `404` — Компания не найдена или пользователь не участник (`#/definitions/swagger.HTTPError`)
- `413` — Запрос больше 2 МиБ (`#/definitions/swagger.HTTPError`)
- `415` — Ожидается multipart/form-data (`#/definitions/swagger.HTTPError`)
- `500` — Internal Server Error (`#/definitions/swagger.HTTPError`)

### `GET /api/v0/companies/{id}/members` — Представители компании

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — id компании

**Ответы:**
- `200` — OK (`array`)
- `400` — Bad Request (`#/definitions/swagger.HTTPError`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `404` — Not Found (`#/definitions/swagger.HTTPError`)

### `POST /api/v0/companies/{id}/members` — Приглашение сотрудника

Руководитель добавляет зарегистрированного клиента по email, без права подписи.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — id компании
- `body request` (обязательный, `#/definitions/companydto.AddMemberRequest`) — Email и должность

**Ответы:**
- `201` — Created (`#/definitions/companydto.MemberResponse`)
- `400` — Bad Request (`#/definitions/swagger.HTTPError`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `403` — не руководитель (`#/definitions/swagger.HTTPError`)
- `404` — нет компании или клиента (`#/definitions/swagger.HTTPError`)
- `409` — уже представитель (`#/definitions/swagger.HTTPError`)

### `DELETE /api/v0/companies/{id}/members/{user_id}` — Удаление представителя

Руководитель удаляет сотрудника, сотрудник может удалить себя сам. Руководителя удалить нельзя.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — id компании
- `path user_id` (обязательный, `string (uuid)`) — id пользователя

**Ответы:**
- `204` — No Content
- `400` — Bad Request (`#/definitions/swagger.HTTPError`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `403` — не руководитель (`#/definitions/swagger.HTTPError`)
- `404` — Not Found (`#/definitions/swagger.HTTPError`)
- `409` — руководителя удалить нельзя (`#/definitions/swagger.HTTPError`)

### `GET /api/v0/companies/{id}/overview` — Сводка по бизнесу из реестра, БКИ и финансовой отчётности

Только для client, состоящего в компании. Источники запрашиваются параллельно по ИНН из БД. Если источник не вернул данные, берётся сохранённый блок той же компании. Если в финансах нет последнего завершённого календарного месяца по Europe/Moscow, недостающие месяцы добавляются из БД; для совпадающих месяцев приоритет у мока. При наличии данных status=ok; без данных сохраняется not_found или unavailable и data=null. Если и в БД нет нужного месяца, отсутствующие данные не выдумываются: возвращается доступный отчёт. Отсутствие снимка не является ошибкой; ошибка чтения БД даёт 500. GET ничего не сохраняет. Денежные суммы передаются в целых копейках.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — ID компании

**Ответы:**
- `200` — OK (`#/definitions/companydto.OverviewResponse`)
- `400` — Bad Request (`#/definitions/swagger.HTTPError`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `403` — Forbidden (`#/definitions/swagger.HTTPError`)
- `404` — Компания не найдена или пользователь не участник (`#/definitions/swagger.HTTPError`)
- `500` — Internal Server Error (`#/definitions/swagger.HTTPError`)

### `POST /api/v0/companies/challenges` — Заявление о подключении компании

Шаг 1 из 2. Возвращает текст заявления (data), который руководитель подписывает КЭП в esign-mock: POST /v1/sign. Заявление одноразовое, живёт 5 минут.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `body request` (обязательный, `#/definitions/companydto.ChallengeRequest`) — ИНН компании

**Ответы:**
- `201` — Created (`#/definitions/companydto.ChallengeResponse`)
- `400` — Bad Request (`#/definitions/swagger.HTTPError`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `404` — нет в реестре (`#/definitions/swagger.HTTPError`)
- `503` — реестр недоступен (`#/definitions/swagger.HTTPError`)

### `GET /api/v0/companies/lookup` — Карточка компании из реестра

Ищет ЮЛ в ЕГРЮЛ или ИП в ЕГРИП по ИНН. Ничего не сохраняет.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `query inn` (обязательный, `string`) — ИНН: 10 цифр ЮЛ или 12 цифр ИП

**Ответы:**
- `200` — OK (`#/definitions/companydto.RegistryCardResponse`)
- `400` — Bad Request (`#/definitions/swagger.HTTPError`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `404` — нет в реестре (`#/definitions/swagger.HTTPError`)
- `503` — реестр недоступен (`#/definitions/swagger.HTTPError`)

## credit-products

### `GET /api/v0/credit-products` — Подбор кредитных продуктов

Возвращает активные продукты, для которых не выявлено нарушений формальных условий. Все query-параметры необязательны: при неполных данных eligibility=needs_details, при полной проверке — eligible. Неподходящие продукты исключаются. Без параметров возвращается активный каталог с причинами недостающих данных; при отсутствии вариантов — []. Это предварительная проверка, а не кредитное решение.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `query purpose` (необязательный, `string`) — Цель кредитования
- `query purpose_description` (необязательный, `string`) — Пояснение для цели other
- `query amount_kopecks` (необязательный, `integer (int64)`) — Сумма в копейках
- `query term_months` (необязательный, `integer`) — Срок аннуитетного кредита в месяцах; для овердрафта не используется
- `query need_period_days` (необязательный, `integer`) — Период отдельного транша овердрафта в днях; для аннуитетного кредита не используется
- `query registration_date` (необязательный, `string (date)`) — Дата регистрации бизнеса: YYYY-MM-DD; возраст рассчитывается по дате Europe/Moscow
- `query collateral_mode` (необязательный, `string`) — required — с залогом, not_supported — без залога, optional — выбор ещё не сделан

**Ответы:**
- `200` — OK (`array`)
- `400` — Некорректный формат параметров или неизвестное значение перечисления (`#/definitions/swagger.HTTPError`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `403` — Forbidden (`#/definitions/swagger.HTTPError`)
- `500` — Internal Server Error (`#/definitions/swagger.HTTPError`)

## documents

### `GET /api/v0/documents/{id}/content` — Файл документа

Клиенту — документы заявок своей компании, сотруднику — любые.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Документ

**Ответы:**
- `200` — OK (`file`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `503` — SERVICE_UNAVAILABLE (`#/definitions/applicationdto.ErrorResponse`)

## underwriter

### `GET /api/v0/underwriter/applications` — Очередь заявок

Заявки на рассмотрении и после него, старые сверху. Черновики не показываются. assignee: me — мои, none — свободные, uuid — сотрудника.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `query status` (необязательный, `string`) — Статус заявки
- `query assignee` (необязательный, `string`) — me, none или id сотрудника
- `query limit` (необязательный, `integer`) — От 1 до 100, по умолчанию 20
- `query cursor` (необязательный, `string`) — next_cursor предыдущей страницы

**Ответы:**
- `200` — OK (`#/definitions/underwritingdto.ApplicationPageResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `403` — не сотрудник (`#/definitions/swagger.HTTPError`)

### `GET /api/v0/underwriter/applications/{id}` — Карточка заявки

Заявка, проверки, скоринг, залог, запросы уточнений, решения и история. decision_blockers — что мешает одобрить заявку на запрошенных условиях.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка

**Ответы:**
- `200` — OK (`#/definitions/underwritingdto.ReviewApplicationResponse`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `403` — не сотрудник (`#/definitions/swagger.HTTPError`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/underwriter/applications/{id}/cash-plan-review` — Заключение по денежному плану

Неизменяемая версия заключения в manual_review или info_requested: accepted, needs_info или rejected с внутренним comment о предпосылках. accepted — с validated_reserve_kopecks от 0 до заявленного резерва: кредитные средства и деньги бюджета проекта в него не входят. terms — условия проверки, по умолчанию из запроса; сумма не больше запрошенной. Принять план можно, только если клиент подтвердил его на срок условий, а у «Старта» сумма сходится с бюджетом, — иначе 422: сначала новая ревизия клиента. Заключение действует, пока не изменились план, условия и data_revision заявки; revision + 1, data_revision не меняется. needs_info сам вопрос клиенту не создаёт — для этого request-info.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `body request` (обязательный, `#/definitions/underwritingdto.CashPlanReviewRequest`) — Заключение

**Ответы:**
- `200` — OK (`#/definitions/underwritingdto.ReviewApplicationResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `403` — FORBIDDEN: заявка у другого сотрудника (`#/definitions/applicationdto.ErrorResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — REVISION_CONFLICT, INVALID_STATE (`#/definitions/applicationdto.ErrorResponse`)
- `422` — VALIDATION_FAILED: нет плана, не подтверждён, условия, резерв (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/underwriter/applications/{id}/decision` — Опубликовать решение

approved — условия как в запросе (ставка не считается), counter_offer — изменён продукт, сумма, срок или обеспечение. Положительное решение требует готовых проверок, принятого залога у обеспеченного продукта и, если нужен денежный план, подтверждённого клиентом и принятого на этих условиях плана, который проходит расчёт; создаёт предложение и PDF договора. Предложение по плану действует не дольше 7 суток и до начала следующего месяца. Платёж считает сервер. rejected — с reason_code и пояснениями для клиента.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `body request` (обязательный, `#/definitions/underwritingdto.DecisionRequest`) — Решение

**Ответы:**
- `200` — OK (`#/definitions/underwritingdto.ReviewApplicationResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `403` — FORBIDDEN: заявка у другого сотрудника (`#/definitions/applicationdto.ErrorResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — REVISION_CONFLICT, INVALID_STATE (`#/definitions/applicationdto.ErrorResponse`)
- `422` — VALIDATION_FAILED: условия, проверки, залог (`#/definitions/applicationdto.ErrorResponse`)
- `503` — SERVICE_UNAVAILABLE: хранилище договоров (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/underwriter/applications/{id}/request-info` — Запросить уточнения

Создаёт запрос клиенту и переводит заявку в info_requested. editable_fields — поля черновика, которые клиент сможет поправить.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `body request` (обязательный, `#/definitions/underwritingdto.RequestInfoRequest`) — Запрос

**Ответы:**
- `200` — OK (`#/definitions/underwritingdto.ReviewApplicationResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `403` — FORBIDDEN: заявка у другого сотрудника (`#/definitions/applicationdto.ErrorResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — REVISION_CONFLICT, INVALID_STATE (`#/definitions/applicationdto.ErrorResponse`)
- `422` — VALIDATION_FAILED (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/underwriter/applications/{id}/retry-checks` — Повторить проверки

Перезапрашивает источники в manual_review; результат придёт асинхронно с новой revision: новые снимки, стоп-правило P06 (может дать rejected) и пересчёт модели.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `body request` (обязательный, `#/definitions/underwritingdto.RetryChecksRequest`) — Источники

**Ответы:**
- `200` — OK (`#/definitions/underwritingdto.ReviewApplicationResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `403` — FORBIDDEN: заявка у другого сотрудника (`#/definitions/applicationdto.ErrorResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — REVISION_CONFLICT, INVALID_STATE (`#/definitions/applicationdto.ErrorResponse`)
- `422` — VALIDATION_FAILED (`#/definitions/applicationdto.ErrorResponse`)
- `503` — SERVICE_UNAVAILABLE (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/underwriter/applications/{id}/retry-scoring` — Пересчитать скоринг

Пересчитывает модель в manual_review на текущих снимках реестра и БКИ и принятых финансах — например, после оценки failed. Результат придёт асинхронно с новой revision.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `body request` (обязательный, `#/definitions/underwritingdto.RevisionRequest`) — Ожидаемая revision

**Ответы:**
- `200` — OK (`#/definitions/underwritingdto.ReviewApplicationResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `403` — FORBIDDEN: заявка у другого сотрудника (`#/definitions/applicationdto.ErrorResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — REVISION_CONFLICT, INVALID_STATE (`#/definitions/applicationdto.ErrorResponse`)

### `POST /api/v0/underwriter/applications/{id}/take` — Взять заявку

Назначает заявку на текущего сотрудника. Повтор тем же сотрудником безопасен; заявку другого сотрудника — 409 INVALID_STATE.

**Авторизация:** требуется Bearer-токен.
**Content-Type:** `application/json`.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — Заявка
- `body request` (обязательный, `#/definitions/underwritingdto.RevisionRequest`) — Версия заявки

**Ответы:**
- `200` — OK (`#/definitions/underwritingdto.ReviewApplicationResponse`)
- `400` — INVALID_REQUEST (`#/definitions/applicationdto.ErrorResponse`)
- `404` — NOT_FOUND (`#/definitions/applicationdto.ErrorResponse`)
- `409` — REVISION_CONFLICT, INVALID_STATE (`#/definitions/applicationdto.ErrorResponse`)

## users

### `POST /api/v0/users` — Создание клиента

Заводит пользователя с ролью client и статусом active, сессию не открывает.

**Авторизация:** не требуется.
**Content-Type:** `application/json`.

**Параметры:**
- `body request` (обязательный, `#/definitions/userdto.CreateClientRequest`) — Данные клиента

**Ответы:**
- `201` — Created (`#/definitions/userdto.UserResponse`)
- `400` — Bad Request (`#/definitions/swagger.HTTPError`)
- `409` — email или телефон уже заняты (`#/definitions/swagger.HTTPError`)
- `500` — Internal Server Error (`#/definitions/swagger.HTTPError`)

### `DELETE /api/v0/users/{id}` — Удаление пользователя

Ставит статус deleted, запись остаётся в БД. Только для admin.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — id пользователя

**Ответы:**
- `204` — No Content
- `400` — Bad Request (`#/definitions/swagger.HTTPError`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `403` — Forbidden (`#/definitions/swagger.HTTPError`)
- `404` — Not Found (`#/definitions/swagger.HTTPError`)
- `500` — Internal Server Error (`#/definitions/swagger.HTTPError`)

### `POST /api/v0/users/{id}/ban` — Бан пользователя

Ставит статус banned. Только для admin.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — id пользователя

**Ответы:**
- `204` — No Content
- `400` — Bad Request (`#/definitions/swagger.HTTPError`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `403` — Forbidden (`#/definitions/swagger.HTTPError`)
- `404` — Not Found (`#/definitions/swagger.HTTPError`)
- `500` — Internal Server Error (`#/definitions/swagger.HTTPError`)

### `POST /api/v0/users/{id}/reset` — Восстановление пользователя

Возвращает статус active после бана или удаления. Только для admin.

**Авторизация:** требуется Bearer-токен.

**Параметры:**
- `path id` (обязательный, `string (uuid)`) — id пользователя

**Ответы:**
- `204` — No Content
- `400` — Bad Request (`#/definitions/swagger.HTTPError`)
- `401` — Unauthorized (`#/definitions/swagger.HTTPError`)
- `403` — Forbidden (`#/definitions/swagger.HTTPError`)
- `404` — Not Found (`#/definitions/swagger.HTTPError`)
- `500` — Internal Server Error (`#/definitions/swagger.HTTPError`)
