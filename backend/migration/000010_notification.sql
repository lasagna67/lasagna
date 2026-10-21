-- +goose Up
-- +goose StatementBegin

CREATE TYPE notification_type AS ENUM (
    'status_changed',
    'info_requested',
    'offer_ready',
    'signature_required'
);

-- Лента уведомлений в кабинете: по условиям кейса весь сценарий;
--
-- проходит внутри продукта, без email/SMS/мессенджеров.
CREATE TABLE notifications (
    id              UUID                PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID                NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    application_id  UUID                REFERENCES applications (id) ON DELETE CASCADE,
    type            notification_type   NOT NULL,
    title           VARCHAR(200)        NOT NULL,
    body            TEXT                NOT NULL DEFAULT '',
    read_at         TIMESTAMPTZ,

    created_at      TIMESTAMPTZ         NOT NULL DEFAULT now()
);

CREATE INDEX notifications_user_id_idx ON notifications (user_id, created_at DESC);
CREATE INDEX notifications_unread_idx  ON notifications (user_id) WHERE read_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS notifications;

DROP TYPE IF EXISTS notification_type;

-- +goose StatementEnd
