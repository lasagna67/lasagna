-- +goose Up
-- +goose StatementBegin

-- ЮЛ (ИНН 10 цифр, ЕГРЮЛ) или ИП (ИНН 12 цифр, ЕГРИП).
CREATE TYPE company_entity_type AS ENUM ('legal_entity', 'individual_entrepreneur');

-- business_status из ЕГРЮЛ/ЕГРИП (registry-mock)
CREATE TYPE company_registry_status AS ENUM ('active', 'reorganizing', 'terminated');

-- owner (ИП) и ceo (руководитель ЮЛ) подтверждаются КЭП, остальных приглашает руководитель.
CREATE TYPE company_member_position AS ENUM (
    'owner',
    'ceo',
    'cfo',
    'accountant',
    'finance_officer',
    'authorized_employee'
);

-- Компания
-- Реквизиты заполняются из реестра; 
-- registry_synced_at — время последней сверки;
CREATE TABLE companies (
    id                          UUID                     PRIMARY KEY DEFAULT gen_random_uuid(),
    inn                         VARCHAR(12)              NOT NULL UNIQUE,
    entity_type                 company_entity_type      NOT NULL,
    ogrn                        VARCHAR(15),
    kpp                         VARCHAR(9),

    short_name                  TEXT                     NOT NULL,
    full_name                   TEXT,

    registration_date           DATE                     NOT NULL,
    region                      TEXT,
    address                     TEXT,
    registry_status             company_registry_status  NOT NULL,
    termination_date            DATE,

    primary_okved               VARCHAR(8),
    additional_okveds           TEXT[]                   NOT NULL DEFAULT '{}',
    authorized_capital_kopecks  BIGINT                   CHECK (authorized_capital_kopecks >= 0),

    -- Руководитель ЮЛ по ЕГРЮЛ (лицо, действующее без доверенности). У ИП пусто.
    head_full_name              TEXT,
    head_position               TEXT,
    head_inn                    VARCHAR(12),

    registry_synced_at          TIMESTAMPTZ,
    created_at                  TIMESTAMPTZ              NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ              NOT NULL DEFAULT now(),

    CONSTRAINT companies_inn_chk CHECK (
        (entity_type = 'legal_entity'            AND inn ~ '^[0-9]{10}$') OR
        (entity_type = 'individual_entrepreneur' AND inn ~ '^[0-9]{12}$')
    )
);

CREATE TRIGGER companies_set_updated_at
    BEFORE UPDATE ON companies
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Связка к users
-- Кто может действовать от имени компании. can_sign — право подписи кредитной документации.
-- Основание связи: certificate_serial — руководитель подтвердил полномочия КЭП;
-- invited_by — сотрудника пригласил руководитель. Право подписи даёт только КЭП.
CREATE TABLE company_members (
    company_id          UUID                      NOT NULL REFERENCES companies (id) ON DELETE CASCADE,
    user_id             UUID                      NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    position            company_member_position   NOT NULL,
    can_sign            BOOLEAN                   NOT NULL DEFAULT false,

    certificate_serial  VARCHAR(64),
    invited_by          UUID                      REFERENCES users (id) ON DELETE SET NULL,

    created_at          TIMESTAMPTZ               NOT NULL DEFAULT now(),

    PRIMARY KEY (company_id, user_id),
    CONSTRAINT company_members_can_sign_chk CHECK (NOT can_sign OR certificate_serial IS NOT NULL)
);

CREATE INDEX company_members_user_id_idx ON company_members (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS company_members;
DROP TABLE IF EXISTS companies;

DROP TYPE IF EXISTS company_member_position;
DROP TYPE IF EXISTS company_registry_status;
DROP TYPE IF EXISTS company_entity_type;

-- +goose StatementEnd
