-- +goose Up
-- +goose StatementBegin

CREATE TYPE document_type AS ENUM (
    'financial_statement',  -- бухгалтерская отчётность
    'bank_statement',       -- выписка по счёту
    'tax_return',           -- налоговая декларация
    'passport',             -- паспорт руководителя / ИП
    'collateral',           -- документы по залогу
    'loan_agreement',       -- кредитный договор, формирует банк
    'other'
);
CREATE TYPE document_status AS ENUM ('uploaded', 'verified', 'rejected');

-- Сам файл лежит в объектном хранилище, здесь — метаданные и ключ.
-- uploaded_by IS NULL — документ сформирован системой (например, договор).
-- signed_* — моделирование подписания.
CREATE TABLE application_documents (
    id                UUID              PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id    UUID              NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    type              document_type     NOT NULL,
    status            document_status   NOT NULL DEFAULT 'uploaded',

    file_name         VARCHAR(255)      NOT NULL,
    content_type      VARCHAR(100)      NOT NULL,
    size_bytes        BIGINT            NOT NULL CHECK (size_bytes > 0),
    storage_key       TEXT              NOT NULL UNIQUE,
    sha256            CHAR(64)          NOT NULL,

    uploaded_by       UUID              REFERENCES users (id),
    rejection_reason  TEXT,

    signed_by         UUID              REFERENCES users (id),
    signed_at         TIMESTAMPTZ,

    created_at        TIMESTAMPTZ       NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ       NOT NULL DEFAULT now(),

    CONSTRAINT application_documents_signed_chk CHECK ((signed_by IS NULL) = (signed_at IS NULL))
);

CREATE INDEX application_documents_application_id_idx ON application_documents (application_id);

CREATE TRIGGER application_documents_set_updated_at
    BEFORE UPDATE ON application_documents
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS application_documents;

DROP TYPE IF EXISTS document_status;
DROP TYPE IF EXISTS document_type;

-- +goose StatementEnd
