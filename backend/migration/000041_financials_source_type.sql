-- +goose Up
ALTER TABLE company_overviews
    ADD COLUMN financials_source_type TEXT NOT NULL DEFAULT 'auto'
    CHECK (financials_source_type IN ('csv', 'auto'));

-- +goose Down
ALTER TABLE company_overviews DROP COLUMN financials_source_type;
