-- +goose Up
-- +goose StatementBegin

-- Рассмотрение заявки сотрудником: запросы уточнений и решение с предложением
-- и договором. Залог и его проверка — application_collateral и collateral_reviews
-- (000044), счёт компании — company_accounts (000042).

-- Пути полей, которые клиент может править, отвечая на запрос (editable_fields контракта).
ALTER TABLE application_info_requests
    ADD COLUMN editable_fields TEXT[] NOT NULL DEFAULT '{}';

-- Решение сотрудника — версия предложения. Новые условия до подписания — новая
-- версия со своим договором; старые хранятся, но новые подписи не принимают.
--
-- annual_rate_bps — сотые доли процента годовых (2150 = 21,50%), заменяет interest_rate.
-- Разовый кредит: term_months и аннуитетный monthly_payment_kopecks.
-- Овердрафт: agreement_term_months, max_tranche_days и счёт, платежа нет.
-- reasons — пояснения для клиента, comment — внутренний комментарий.
-- changed_fields — отличия от запроса: product_code, amount_kopecks, term_months, collateral_mode.
-- contract_document_id — PDF договора в application_documents; у отказа его нет.
ALTER TABLE credit_decisions
    ADD COLUMN version                INTEGER,
    ADD COLUMN annual_rate_bps        INTEGER    CHECK (annual_rate_bps >= 0),
    ADD COLUMN agreement_term_months  SMALLINT   CHECK (agreement_term_months > 0),
    ADD COLUMN max_tranche_days       SMALLINT   CHECK (max_tranche_days > 0),
    ADD COLUMN bank_account_id        UUID       REFERENCES company_accounts (id),
    ADD COLUMN collateral_required    BOOLEAN    NOT NULL DEFAULT false,
    ADD COLUMN changed_fields         TEXT[]     NOT NULL DEFAULT '{}',
    ADD COLUMN reason_code            VARCHAR(64),
    ADD COLUMN contract_document_id   UUID       UNIQUE REFERENCES application_documents (id);

UPDATE credit_decisions AS d
SET version = v.n,
    annual_rate_bps = round(d.interest_rate * 100)
FROM (
    SELECT id, row_number() OVER (PARTITION BY application_id ORDER BY created_at, id) AS n
    FROM credit_decisions
) AS v
WHERE v.id = d.id;

ALTER TABLE credit_decisions
    ALTER COLUMN version SET NOT NULL,
    ADD CONSTRAINT credit_decisions_version_chk CHECK (version > 0),
    ADD CONSTRAINT credit_decisions_application_version_key UNIQUE (application_id, version),
    DROP CONSTRAINT credit_decisions_terms_chk,
    DROP COLUMN interest_rate;

-- NOT VALID: правило для новых решений; ранние записи без договора не ломают миграцию.
ALTER TABLE credit_decisions ADD CONSTRAINT credit_decisions_terms_chk CHECK (
    (outcome = 'rejected' AND reason_code IS NOT NULL AND contract_document_id IS NULL)
    OR
    (outcome <> 'rejected'
     AND product_id IS NOT NULL AND amount_kopecks IS NOT NULL
     AND annual_rate_bps IS NOT NULL AND contract_document_id IS NOT NULL
     AND (
        (term_months IS NOT NULL AND monthly_payment_kopecks IS NOT NULL
         AND agreement_term_months IS NULL AND max_tranche_days IS NULL)
        OR
        (term_months IS NULL AND monthly_payment_kopecks IS NULL
         AND agreement_term_months IS NOT NULL AND max_tranche_days IS NOT NULL
         AND bank_account_id IS NOT NULL)
     ))
) NOT VALID;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE credit_decisions
    DROP CONSTRAINT credit_decisions_terms_chk,
    ADD COLUMN interest_rate NUMERIC(5, 2) CHECK (interest_rate > 0);

UPDATE credit_decisions SET interest_rate = annual_rate_bps / 100.0 WHERE annual_rate_bps > 0;

ALTER TABLE credit_decisions
    DROP CONSTRAINT credit_decisions_application_version_key,
    DROP CONSTRAINT credit_decisions_version_chk,
    DROP COLUMN contract_document_id,
    DROP COLUMN reason_code,
    DROP COLUMN changed_fields,
    DROP COLUMN collateral_required,
    DROP COLUMN bank_account_id,
    DROP COLUMN max_tranche_days,
    DROP COLUMN agreement_term_months,
    DROP COLUMN annual_rate_bps,
    DROP COLUMN version,
    ADD CONSTRAINT credit_decisions_terms_chk CHECK (
        outcome = 'rejected' OR (
            product_id      IS NOT NULL AND
            amount_kopecks  IS NOT NULL AND
            term_months     IS NOT NULL AND
            interest_rate   IS NOT NULL
        )
    ) NOT VALID;

ALTER TABLE application_info_requests DROP COLUMN editable_fields;

-- +goose StatementEnd
