-- +goose Up
-- +goose StatementBegin

-- Новая цель коммитится до следующей миграции, которая использует её в каталоге.
-- business_launch сохраняется для обратной совместимости.
ALTER TYPE credit_purpose ADD VALUE IF NOT EXISTS 'business_start';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- PostgreSQL не поддерживает удаление отдельного значения ENUM.
-- Оставляем business_start: оно уже может использоваться в заявках.
SELECT 1;

-- +goose StatementEnd
