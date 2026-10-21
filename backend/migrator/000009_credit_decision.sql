-- +goose Up
-- +goose StatementBegin

CREATE TYPE credit_decision_outcome  AS ENUM ('approved', 'counter_offer', 'rejected');
CREATE TYPE credit_decision_response AS ENUM ('accepted', 'declined');

-- Решение по заявке. Их может быть несколько (авто, затем ручное) — актуальное последнее.
--
-- decided_by IS NULL — решение принято автоматически.
-- product_id может отличаться от запрошенного: банк предлагает другой продукт.
-- reasons — коды причин, например {HIGH_DEBT_LOAD, NEGATIVE_CREDIT_HISTORY}.
CREATE TABLE credit_decisions (
    id                        UUID                       PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id            UUID                       NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    scoring_result_id         UUID                       REFERENCES scoring_results (id),
    outcome                   credit_decision_outcome    NOT NULL,
    decided_by                UUID                       REFERENCES users (id),

    product_id                UUID                       REFERENCES credit_products (id),
    amount_kopecks            BIGINT                     CHECK (amount_kopecks > 0),
    term_months               SMALLINT                   CHECK (term_months > 0),
    interest_rate             NUMERIC(5, 2)              CHECK (interest_rate > 0),
    monthly_payment_kopecks   BIGINT                     CHECK (monthly_payment_kopecks > 0),

    reasons                   TEXT[]                     NOT NULL DEFAULT '{}',
    comment                   TEXT,
    valid_until               TIMESTAMPTZ,

    client_response           credit_decision_response,
    responded_at              TIMESTAMPTZ,

    created_at                TIMESTAMPTZ                NOT NULL DEFAULT now(),

    CONSTRAINT credit_decisions_terms_chk CHECK (
        outcome = 'rejected' OR (
            product_id      IS NOT NULL AND
            amount_kopecks  IS NOT NULL AND
            term_months     IS NOT NULL AND
            interest_rate   IS NOT NULL
        )
    ),
    CONSTRAINT credit_decisions_response_chk CHECK (
        (client_response IS NULL) = (responded_at IS NULL) AND
        (outcome <> 'rejected' OR client_response IS NULL)
    )
);

CREATE INDEX credit_decisions_application_id_idx ON credit_decisions (application_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS credit_decisions;

DROP TYPE IF EXISTS credit_decision_response;
DROP TYPE IF EXISTS credit_decision_outcome;

-- +goose StatementEnd
