-- +goose Up
-- +goose StatementBegin

-- Техническая история конвейера (B03): события, которые этап отбросил, —
-- устаревший запуск (заявку отправили заново), уже пройденный этап при повторной
-- доставке, заявка вне статуса этапа. Клиенту не показывается; заявку не меняет.
CREATE TABLE pipeline_skipped_events (
    id              BIGINT        GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    application_id  UUID          NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    run_id          UUID          NOT NULL,
    topic           TEXT          NOT NULL,
    reason          TEXT          NOT NULL,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE INDEX pipeline_skipped_events_application_idx ON pipeline_skipped_events (application_id, created_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE pipeline_skipped_events;

-- +goose StatementEnd
