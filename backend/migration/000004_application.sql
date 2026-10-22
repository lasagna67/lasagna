-- +goose Up
-- +goose StatementBegin

CREATE TYPE application_status AS ENUM (
    'draft',           -- клиент заполняет, данные сохраняются по шагам
    'submitted',       -- отправлена, ждёт обработки конвейером
    'validation',      -- проверка полноты и корректности
    'info_requested',  -- банк запросил уточнения у клиента
    'enrichment',      -- запросы в ЕГРЮЛ/ЕГРИП и БКИ
    'scoring',         -- расчёт оценки риска
    'manual_review',   -- нестандартный случай, у андеррайтера
    'approved',        -- одобрено на запрошенных условиях
    'counter_offer',   -- одобрено на иных условиях
    'rejected',        -- отказ
    'signing',         -- клиент принял предложение, подписание документов
    'issued',          -- средства выданы (моделируется)
    'cancelled',       -- отозвана клиентом или он отказался от предложения
    'expired'          -- предложение не принято в срок
);

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
    purpose                     credit_purpose,
    purpose_description         TEXT,
    requested_amount_kopecks    BIGINT               CHECK (requested_amount_kopecks > 0),
    requested_term_months       SMALLINT             CHECK (requested_term_months > 0),

    annual_revenue_kopecks      BIGINT               CHECK (annual_revenue_kopecks >= 0),
    annual_net_profit_kopecks   BIGINT,
    existing_debt_kopecks       BIGINT               CHECK (existing_debt_kopecks >= 0),

    has_collateral              BOOLEAN              NOT NULL DEFAULT false,
    collateral_description      TEXT,
    collateral_value_kopecks    BIGINT               CHECK (collateral_value_kopecks > 0),

    -- андеррайтер, взявший заявку на ручное рассмотрение
    assignee_id                 UUID                 REFERENCES users (id),

    submitted_at                TIMESTAMPTZ,
    created_at                  TIMESTAMPTZ          NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ          NOT NULL DEFAULT now(),

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
-- actor_id IS NULL — переход выполнил конвейер автоматически.
CREATE TABLE application_status_history (
    id              BIGINT               GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    application_id  UUID                 NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    from_status     application_status,
    to_status       application_status   NOT NULL,
    actor_id        UUID                 REFERENCES users (id),
    comment         TEXT,

    created_at      TIMESTAMPTZ          NOT NULL DEFAULT now()
);

CREATE INDEX application_status_history_application_id_idx
    ON application_status_history (application_id, created_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS application_status_history;
DROP TABLE IF EXISTS applications;

DROP TYPE IF EXISTS application_status;

-- +goose StatementEnd
