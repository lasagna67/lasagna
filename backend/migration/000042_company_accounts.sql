-- +goose Up
-- Синтетическая нумерация, не реквизиты реального банка.
CREATE SEQUENCE company_account_number_seq MAXVALUE 999999999999;

CREATE TABLE company_accounts (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id       UUID NOT NULL UNIQUE REFERENCES companies (id) ON DELETE CASCADE,
    title            TEXT NOT NULL DEFAULT 'Синтетический расчётный счёт',
    account_number   VARCHAR(20) NOT NULL UNIQUE
                     DEFAULT ('40702810' || lpad(nextval('company_account_number_seq')::text, 12, '0')),
    currency         TEXT NOT NULL DEFAULT 'RUB' CHECK (currency = 'RUB'),
    status           TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    balance_kopecks  BIGINT NOT NULL DEFAULT 0 CHECK (balance_kopecks >= 0),
    CHECK (account_number ~ '^[0-9]{20}$')
);

ALTER SEQUENCE company_account_number_seq OWNED BY company_accounts.account_number;

-- +goose Down
DROP TABLE company_accounts;
