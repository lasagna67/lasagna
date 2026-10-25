-- +goose Up
-- +goose StatementBegin

-- Денежный план (P09) и проект «Старт» (P11): данные клиента, его подтверждение
-- и неизменяемые заключения банка по плану.

-- data_revision — версия входных данных риска (C01): растёт при изменении запроса,
-- залога, полей проекта и денежного плана, но не статуса, назначения и банковских
-- заключений. Заключение по плану действует только для своей data_revision.
ALTER TABLE applications
    ADD COLUMN data_revision INTEGER NOT NULL DEFAULT 1 CHECK (data_revision > 0);

-- product_details «Старта». Черновик сохраняется частично: полноту и бюджет
-- (кредит + собственные средства = стоимость статей) проверяет отправка.
CREATE TABLE application_projects (
    application_id     UUID    PRIMARY KEY REFERENCES applications (id) ON DELETE CASCADE,
    description        TEXT    CHECK (btrim(description) <> '' AND char_length(description) <= 3000),
    cost_items         JSONB   NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(cost_items) = 'array'),
    own_funds_kopecks  BIGINT  CHECK (own_funds_kopecks >= 0),
    sales_start_month  TEXT    CHECK (sales_start_month ~ '^[0-9]{4}-(0[1-9]|1[0-2])$')
);

-- Денежный план: прогноз помесячно с first_month. confirmed_hash — хеш содержимого,
-- которое подтвердил клиент; после правки он не совпадает с текущим, и подтверждения нет.
CREATE TABLE application_cash_plans (
    application_id       UUID    PRIMARY KEY REFERENCES applications (id) ON DELETE CASCADE,
    kind                 TEXT    NOT NULL CHECK (kind IN ('project', 'liquidity')),
    first_month          TEXT    NOT NULL CHECK (first_month ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
    own_reserve_kopecks  BIGINT  NOT NULL CHECK (own_reserve_kopecks >= 0),
    forecast             JSONB   NOT NULL CHECK (jsonb_typeof(forecast) = 'array' AND jsonb_array_length(forecast) <= 120),
    confirmed_hash       TEXT    CHECK (confirmed_hash ~ '^[0-9a-f]{64}$'),
    confirmed_by         UUID    REFERENCES users (id),
    confirmed_at         TIMESTAMPTZ,
    CONSTRAINT application_cash_plans_confirmed_chk CHECK (
        (confirmed_hash IS NULL) = (confirmed_by IS NULL) AND (confirmed_hash IS NULL) = (confirmed_at IS NULL)
    )
);

-- Заключение банка по плану. Действует, пока совпадают хеш плана, хеш условий
-- (продукт, сумма, срок, обеспечение) и data_revision заявки. accepted — с
-- проверенным резервом не больше заявленного клиентом.
CREATE TABLE cash_plan_reviews (
    id                         UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id             UUID         NOT NULL REFERENCES applications (id),
    revision                   INTEGER      NOT NULL CHECK (revision > 0),
    version                    INTEGER      NOT NULL CHECK (version > 0),
    reviewer                   UUID         NOT NULL REFERENCES users (id),
    created_at                 TIMESTAMPTZ  NOT NULL DEFAULT now(),
    cash_plan_hash             TEXT         NOT NULL CHECK (cash_plan_hash ~ '^[0-9a-f]{64}$'),
    terms                      JSONB        NOT NULL CHECK (jsonb_typeof(terms) = 'object'),
    terms_hash                 TEXT         NOT NULL CHECK (terms_hash ~ '^[0-9a-f]{64}$'),
    data_revision              INTEGER      NOT NULL CHECK (data_revision > 0),
    result                     TEXT         NOT NULL CHECK (result IN ('accepted', 'needs_info', 'rejected')),
    validated_reserve_kopecks  BIGINT       CHECK (validated_reserve_kopecks >= 0),
    comment                    TEXT         NOT NULL CHECK (btrim(comment) <> ''),
    public_comment             TEXT,
    CONSTRAINT cash_plan_reviews_reserve_chk CHECK ((result = 'accepted') = (validated_reserve_kopecks IS NOT NULL)),
    UNIQUE (application_id, version)
);

CREATE FUNCTION prevent_cash_plan_review_change() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'cash plan reviews are immutable' USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER cash_plan_reviews_immutable
    BEFORE UPDATE OR DELETE ON cash_plan_reviews
    FOR EACH ROW EXECUTE FUNCTION prevent_cash_plan_review_change();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE cash_plan_reviews;
DROP FUNCTION prevent_cash_plan_review_change();
DROP TABLE application_cash_plans;
DROP TABLE application_projects;
ALTER TABLE applications DROP COLUMN data_revision;

-- +goose StatementEnd
