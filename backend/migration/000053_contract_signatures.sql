-- +goose Up
CREATE TABLE contract_signing_contexts (
    id UUID PRIMARY KEY,
    application_id UUID NOT NULL REFERENCES applications(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id),
    company_id UUID NOT NULL REFERENCES companies(id),
    expected_revision INTEGER NOT NULL CHECK (expected_revision > 0),
    offer_id UUID NOT NULL REFERENCES credit_decisions(id),
    document_id UUID NOT NULL REFERENCES application_documents(id),
    document_sha256 CHAR(64) NOT NULL,
    certificate_serial VARCHAR(64) NOT NULL,
    nonce TEXT NOT NULL UNIQUE,
    evidence_sha256 CHAR(64) NOT NULL,
    signed_data TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ
);

CREATE TABLE contract_signatures (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    context_id UUID NOT NULL UNIQUE REFERENCES contract_signing_contexts(id),
    application_id UUID NOT NULL UNIQUE REFERENCES applications(id) ON DELETE CASCADE,
    offer_id UUID NOT NULL REFERENCES credit_decisions(id),
    document_id UUID NOT NULL REFERENCES application_documents(id),
    document_sha256 CHAR(64) NOT NULL,
    signed_data TEXT NOT NULL,
    signature TEXT NOT NULL,
    certificate JSONB NOT NULL,
    signed_by UUID NOT NULL REFERENCES users(id),
    signed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    modeled_effective_date DATE GENERATED ALWAYS AS ((signed_at AT TIME ZONE 'Europe/Moscow')::date) STORED,
    terms_accepted BOOLEAN NOT NULL CHECK(terms_accepted)
);

-- Устойчивая передача принятого договора модулю выдачи, одна операция на заявку.
CREATE TABLE application_issue_jobs (
    application_id UUID PRIMARY KEY REFERENCES applications(id) ON DELETE CASCADE,
    signature_id UUID NOT NULL UNIQUE REFERENCES contract_signatures(id),
    operation_id UUID NOT NULL UNIQUE DEFAULT gen_random_uuid(),
    status TEXT NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','processing','completed','failed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE application_issue_jobs;
DROP TABLE contract_signatures;
DROP TABLE contract_signing_contexts;
