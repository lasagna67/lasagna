-- +goose Up
ALTER TABLE contract_signatures
    ADD COLUMN signed_document_id UUID UNIQUE REFERENCES application_documents(id);

-- +goose Down
ALTER TABLE contract_signatures DROP COLUMN signed_document_id;
