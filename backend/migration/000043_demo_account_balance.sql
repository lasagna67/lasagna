-- +goose Up
ALTER TABLE company_accounts
    ADD COLUMN demo_balance_seeded BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE company_accounts DROP COLUMN demo_balance_seeded;
