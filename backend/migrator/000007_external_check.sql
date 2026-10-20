-- +goose Up
-- +goose StatementBegin

-- registry — ЕГРЮЛ/ЕГРИП (registry-mock), credit_bureau — БКИ (bki-mock).
CREATE TYPE external_check_source AS ENUM ('registry', 'credit_bureau');
CREATE TYPE external_check_status AS ENUM ('pending', 'completed', 'failed');

-- Снимок ответа внешнего источника на момент рассмотрения заявки,
-- чтобы решение можно было воспроизвести и объяснить.
-- Повторная проверка (например, после уточнений клиента) — новая строка.
CREATE TABLE external_checks (
    id              UUID                    PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id  UUID                    NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    source          external_check_source   NOT NULL,
    status          external_check_status   NOT NULL DEFAULT 'pending',
    attempts        SMALLINT                NOT NULL DEFAULT 0 CHECK (attempts >= 0),

    response        JSONB,
    error           TEXT,

    requested_at    TIMESTAMPTZ             NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ
);

CREATE INDEX external_checks_application_id_idx
    ON external_checks (application_id, source, requested_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS external_checks;

DROP TYPE IF EXISTS external_check_status;
DROP TYPE IF EXISTS external_check_source;

-- +goose StatementEnd
