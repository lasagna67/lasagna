-- +goose Up
-- +goose StatementBegin

-- Источник истины — creditdomain.Status: порядок и значения совпадают.
CREATE TYPE application_status AS ENUM (
    'draft',           -- клиент заполняет, данные сохраняются по шагам
    'submitted',       -- версия зафиксирована, ждёт конвейера
    'validation',      -- проверка состава данных и условий продукта
    'enrichment',      -- реестр и БКИ для версии заявки
    'scoring',         -- вызов модели; без правил — ручной маршрут
    'manual_review',   -- у сотрудника: материалы, залог, решение
    'info_requested',  -- клиент отвечает в той же заявке
    'approved',        -- предложение на запрошенных условиях
    'counter_offer',   -- предложение на иных условиях
    'signing',         -- договор подписан, ожидается выдача
    'issued',          -- выдача или активация лимита завершена
    'rejected',        -- отказ сотрудника или stop-правила
    'cancelled',       -- клиент отказался от предложения
    'expired'          -- предложение не подписано до valid_until
);

-- Единица периода потребности: месяцы для разового кредита, дни для овердрафта.
CREATE TYPE need_period_unit AS ENUM ('months', 'days', 'unknown');

-- Основной домейн - проход состояний заявки
--
-- Черновик может быть заполнен частично; к моменту отправки (submitted_at)
-- обязательные параметры запроса должны быть заданы.
-- *Финансовые показатели — самостоятельная оценка клиента на момент подачи.
CREATE TABLE applications (
    id                          UUID                 PRIMARY KEY DEFAULT gen_random_uuid(),
    number                      BIGINT               GENERATED ALWAYS AS IDENTITY (START WITH 1000) UNIQUE,

    -- Связки к таблицам
    company_id                  UUID                 NOT NULL REFERENCES companies (id),
    applicant_id                UUID                 NOT NULL REFERENCES users (id),
    product_id                  UUID                 REFERENCES credit_products (id),

    status                      application_status   NOT NULL DEFAULT 'draft',

    -- revision - optimistic locking: каждое изменение заявки увеличивает её на 1.
    revision                    INTEGER              NOT NULL DEFAULT 1 CHECK (revision > 0),
    purpose                     credit_purpose,
    purpose_description         TEXT,

    -- need_period хранится отдельно от срока договора requested_term_months.
    requested_amount_kopecks    BIGINT               CHECK (requested_amount_kopecks > 0),
    requested_term_months       SMALLINT             CHECK (requested_term_months > 0),
    need_period_unit            need_period_unit     NOT NULL DEFAULT 'unknown',
    need_period_value           SMALLINT             CHECK (need_period_value > 0),

    annual_revenue_kopecks      BIGINT               CHECK (annual_revenue_kopecks >= 0),
    annual_net_profit_kopecks   BIGINT,
    existing_debt_kopecks       BIGINT               CHECK (existing_debt_kopecks >= 0),

    -- request.collateral_mode хранится в has_collateral: true — pledged.
    has_collateral              BOOLEAN              NOT NULL DEFAULT false,
    collateral_description      TEXT,
    collateral_value_kopecks    BIGINT               CHECK (collateral_value_kopecks > 0),

    -- андеррайтер, взявший заявку на ручное рассмотрение
    assignee_id                 UUID                 REFERENCES users (id),

    submitted_at                TIMESTAMPTZ,
    created_at                  TIMESTAMPTZ          NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ          NOT NULL DEFAULT now(),

    CONSTRAINT applications_need_period_chk CHECK (need_period_unit <> 'unknown' OR need_period_value IS NULL),
    CONSTRAINT applications_submitted_chk CHECK (
        submitted_at IS NULL OR (
            product_id               IS NOT NULL AND
            purpose                  IS NOT NULL AND
            requested_amount_kopecks IS NOT NULL AND
            requested_term_months    IS NOT NULL
        )
    )
);

CREATE INDEX applications_company_id_idx  ON applications (company_id, created_at DESC);
CREATE INDEX applications_applicant_id_idx ON applications (applicant_id);
CREATE INDEX applications_status_idx      ON applications (status, submitted_at);
CREATE INDEX applications_assignee_id_idx ON applications (assignee_id) WHERE assignee_id IS NOT NULL;

CREATE TRIGGER applications_set_updated_at
    BEFORE UPDATE ON applications
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Журнал переходов: таймлайн для клиента и аудит для банка.
CREATE TABLE application_status_history (
    id              BIGINT               GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    application_id  UUID                 NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    from_status     application_status,
    to_status       application_status   NOT NULL,
    
    -- actor_id IS NULL — переход выполнил конвейер автоматически.
    actor_id        UUID                 REFERENCES users (id),
    comment         TEXT,

    created_at      TIMESTAMPTZ          NOT NULL DEFAULT now()
);

CREATE INDEX application_status_history_application_id_idx ON application_status_history (application_id, created_at);

-- Результаты идемпотентных операций: создание, отправка, resubmit, подпись, повтор выдачи.
--
-- Область — пользователь, операция и ресурс; тот же ключ с другим телом запроса — конфликт.
-- result_id — созданный или изменённый объект, по нему повтор отдаёт прежний результат.
CREATE TABLE idempotency_keys (
    user_id          UUID           NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    operation        VARCHAR(50)    NOT NULL,
    resource_id      UUID, -- resource_id IS NULL — операция создаёт ресурс (создание заявки).
    idempotency_key  VARCHAR(255)   NOT NULL,
    request_sha256   CHAR(64)       NOT NULL CHECK (request_sha256 ~ '^[0-9a-f]{64}$'),
    result_id        UUID           NOT NULL,

    created_at       TIMESTAMPTZ    NOT NULL DEFAULT now(),

    CONSTRAINT idempotency_keys_scope_key UNIQUE NULLS NOT DISTINCT (user_id, operation, resource_id, idempotency_key)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS application_status_history;
DROP TABLE IF EXISTS applications;

DROP TYPE IF EXISTS need_period_unit;
DROP TYPE IF EXISTS application_status;

-- +goose StatementEnd
