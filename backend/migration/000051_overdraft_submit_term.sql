-- +goose Up
-- +goose StatementBegin

-- У овердрафта нет срока кредита: клиент указывает период использования в днях,
-- срок соглашения задаёт банк в решении (credit_decisions.agreement_term_months).
-- Поэтому при отправке срок обязателен, только если период не в днях.
ALTER TABLE applications
    DROP CONSTRAINT applications_submitted_chk,
    ADD CONSTRAINT applications_submitted_chk CHECK (
        submitted_at IS NULL OR (
            product_id               IS NOT NULL AND
            purpose                  IS NOT NULL AND
            requested_amount_kopecks IS NOT NULL AND
            (requested_term_months IS NOT NULL OR need_period_unit = 'days')
        )
    );

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Не применится, если есть отправленные заявки на овердрафт без срока.
ALTER TABLE applications
    DROP CONSTRAINT applications_submitted_chk,
    ADD CONSTRAINT applications_submitted_chk CHECK (
        submitted_at IS NULL OR (
            product_id               IS NOT NULL AND
            purpose                  IS NOT NULL AND
            requested_amount_kopecks IS NOT NULL AND
            requested_term_months    IS NOT NULL
        )
    );

-- +goose StatementEnd
