-- +goose Up
-- +goose StatementBegin

-- Намерение кредита
CREATE TYPE credit_purpose AS ENUM (
    'working_capital',
    'cash_gap',
    'equipment',
    'business_launch',
    'business_start',
    'business_expansion',
    'refinancing',
    'other'
);

CREATE TYPE collateral_mode AS ENUM ('not_supported', 'optional', 'required');
CREATE TYPE repayment_type AS ENUM ('annuity', 'overdraft');

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
    base_rate                NUMERIC(5, 2)     CHECK (base_rate > 0),

    collateral_mode          collateral_mode   NOT NULL,
    repayment_type           repayment_type    NOT NULL,
    max_drawdown_days        SMALLINT,
    min_business_age_months  SMALLINT          NOT NULL DEFAULT 0 CHECK (min_business_age_months >= 0),
    is_active                BOOLEAN           NOT NULL DEFAULT true,

    created_at               TIMESTAMPTZ       NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ       NOT NULL DEFAULT now(),

    CONSTRAINT credit_products_amount_chk CHECK (max_amount_kopecks >= min_amount_kopecks),
    CONSTRAINT credit_products_term_chk   CHECK (max_term_months >= min_term_months),
    CONSTRAINT credit_products_repayment_chk CHECK (
        (repayment_type = 'annuity' AND max_drawdown_days IS NULL)
        OR
        (repayment_type = 'overdraft'
         AND min_term_months = 12 AND max_term_months = 12
         AND max_drawdown_days IS NOT NULL AND max_drawdown_days = 60)
    )
);

CREATE TRIGGER credit_products_set_updated_at
    BEFORE UPDATE ON credit_products
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


-- Каталог по ТЗ. Ставки не согласованы: NULL означает, что расчёт платежа недоступен.
-- Для овердрафта 12 месяцев — срок лимита, 60 дней — максимальный срок транша.
INSERT INTO credit_products
    (code, name, description, purposes,
    min_amount_kopecks, max_amount_kopecks, min_term_months, max_term_months, base_rate,
    collateral_mode, repayment_type, max_drawdown_days, min_business_age_months)
VALUES
    ('express', 'Экспресс-кредит',
     'Небольшой кредит на текущие потребности без залога',
     '{working_capital,cash_gap,other}',
     10000000, 500000000, 3, 36, NULL, 'not_supported', 'annuity', NULL, 6),

    ('overdraft', 'Овердрафт',
     'Лимит на 12 месяцев для покрытия кассовых разрывов; отдельный транш — до 60 дней',
     '{cash_gap,working_capital}',
     10000000, 1000000000, 12, 12, NULL, 'not_supported', 'overdraft', 60, 6),

    ('working_capital', 'Оборотные средства',
     'Финансирование текущих закупок и расчётов с залогом или без него',
     '{working_capital}',
     50000000, 5000000000, 6, 60, NULL, 'optional', 'annuity', NULL, 12),

    ('equipment', 'Оборудование и транспорт',
     'Приобретение оборудования и транспорта с обязательным залогом для выдачи',
     '{equipment}',
     100000000, 10000000000, 12, 120, NULL, 'required', 'annuity', NULL, 12),

    ('investment', 'Инвестиционный кредит «Старт»',
     'Первоначальные вложения в запуск и развитие бизнеса, приобретение оборудования',
     '{business_launch,equipment,business_expansion}',
     10000000, 300000000, 12, 60, NULL, 'required', 'annuity', NULL, 0);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS credit_products;

DROP TYPE IF EXISTS repayment_type;
DROP TYPE IF EXISTS collateral_mode;
DROP TYPE IF EXISTS credit_purpose;

-- +goose StatementEnd
