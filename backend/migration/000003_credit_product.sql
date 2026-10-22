-- +goose Up
-- +goose StatementBegin

-- Намерение кредита
CREATE TYPE credit_purpose AS ENUM (
    'working_capital',
    'cash_gap',
    'equipment',
    'business_expansion',
    'refinancing',
    'other'
);

-- Кредитный продукт - тип кредита
-- Суммы везде в копейках (BIGINT), ставка — % годовых.
-- purposes — под какие цели подходит продукт, используется для подбора.
CREATE TABLE credit_products (
    id                       UUID              PRIMARY KEY DEFAULT gen_random_uuid(),
    code                     VARCHAR(50)       NOT NULL UNIQUE,
    name                     VARCHAR(200)      NOT NULL,
    description              TEXT              NOT NULL DEFAULT '',
    purposes                 credit_purpose[]  NOT NULL DEFAULT '{}',

    min_amount_kopecks       BIGINT            NOT NULL CHECK (min_amount_kopecks > 0),
    max_amount_kopecks       BIGINT            NOT NULL,
    min_term_months          SMALLINT          NOT NULL CHECK (min_term_months > 0),
    max_term_months          SMALLINT          NOT NULL,
    base_rate                NUMERIC(5, 2)     NOT NULL CHECK (base_rate > 0),

    collateral_required      BOOLEAN           NOT NULL DEFAULT false,
    min_business_age_months  SMALLINT          NOT NULL DEFAULT 0 CHECK (min_business_age_months >= 0),
    is_active                BOOLEAN           NOT NULL DEFAULT true,

    created_at               TIMESTAMPTZ       NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ       NOT NULL DEFAULT now(),

    CONSTRAINT credit_products_amount_chk CHECK (max_amount_kopecks >= min_amount_kopecks),
    CONSTRAINT credit_products_term_chk   CHECK (max_term_months >= min_term_months)
);

CREATE TRIGGER credit_products_set_updated_at
    BEFORE UPDATE ON credit_products
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Синтетический каталог продуктов для демо.
INSERT INTO credit_products
    (code, name, description, purposes,
    min_amount_kopecks, max_amount_kopecks, min_term_months, max_term_months, base_rate,
    collateral_required, min_business_age_months)
VALUES
    ('express', 'Экспресс-кредит',
     'Небольшая сумма без залога с решением в день обращения',
     '{working_capital,cash_gap,other}',
     10000000, 500000000, 3, 36, 22.90, false, 6),

    ('overdraft', 'Овердрафт',
     'Покрытие кассовых разрывов по расчётному счёту',
     '{cash_gap,working_capital}',
     10000000, 1000000000, 1, 12, 21.50, false, 6),

    ('working_capital', 'Кредит на пополнение оборотных средств',
     'Закупка товаров и сырья, расчёты с поставщиками',
     '{working_capital}',
     50000000, 5000000000, 6, 36, 19.90, false, 12),

    ('equipment', 'Кредит на оборудование и транспорт',
     'Покупка оборудования и транспорта, приобретаемое имущество — в залог',
     '{equipment}',
     100000000, 10000000000, 12, 84, 18.50, true, 12),

    ('investment', 'Инвестиционный кредит',
     'Развитие бизнеса, запуск новых направлений, рефинансирование',
     '{business_expansion,equipment,refinancing}',
     500000000, 30000000000, 12, 120, 17.90, true, 24);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS credit_products;

DROP TYPE IF EXISTS credit_purpose;

-- +goose StatementEnd
