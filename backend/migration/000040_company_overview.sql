-- +goose Up
-- +goose StatementBegin

-- Последний явно сохранённый снимок overview по компании.
-- Все колонки nullable: недоступность источника не мешает сохранить остальные.
-- UNIQUE связывает снимок с компанией без неявного NOT NULL от PRIMARY KEY.
-- JSONB хранит полные доменные данные, включая кредиты и помесячную отчётность.
CREATE TABLE company_overviews (
    company_id             UUID UNIQUE REFERENCES companies (id) ON DELETE CASCADE,
    registry_status        TEXT,
    registry_data          JSONB,
    credit_history_status  TEXT,
    credit_history_data    JSONB,
    financials_status      TEXT,
    financials_data        JSONB,
    saved_at               TIMESTAMPTZ DEFAULT now()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS company_overviews;

-- +goose StatementEnd
