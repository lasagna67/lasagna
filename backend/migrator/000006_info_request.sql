-- +goose Up
-- +goose StatementBegin

CREATE TYPE info_request_status AS ENUM ('open', 'answered', 'cancelled');

-- Запрос уточнений по заявке (статус info_requested).
--
-- requested_by IS NULL — запрос сформирован автоматической проверкой.
-- document_type задан, если для ответа нужно загрузить конкретный документ.
CREATE TABLE application_info_requests (
    id              UUID                  PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id  UUID                  NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    status          info_request_status   NOT NULL DEFAULT 'open',

    requested_by    UUID                  REFERENCES users (id),
    message         TEXT                  NOT NULL,
    document_type   document_type,

    answered_by     UUID                  REFERENCES users (id),
    answer          TEXT,
    answered_at     TIMESTAMPTZ,

    created_at      TIMESTAMPTZ           NOT NULL DEFAULT now(),

    CONSTRAINT application_info_requests_answered_chk CHECK (
        (status = 'answered') = (answered_at IS NOT NULL)
    )
);

CREATE INDEX application_info_requests_application_id_idx
    ON application_info_requests (application_id, created_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS application_info_requests;

DROP TYPE IF EXISTS info_request_status;

-- +goose StatementEnd
