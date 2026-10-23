-- +goose Up
-- +goose StatementBegin

-- У существующих продуктов сохраняем ID и описания; параметры берём из ТЗ.
UPDATE credit_products AS p
SET name = spec.name,
    min_amount_kopecks = spec.min_amount,
    max_amount_kopecks = spec.max_amount,
    min_term_months = spec.min_term,
    max_term_months = spec.max_term,
    base_rate = spec.rate,
    purposes = spec.purposes::credit_purpose[],
    collateral_mode = spec.collateral::collateral_mode,
    repayment_type = spec.repayment::repayment_type,
    max_drawdown_days = spec.drawdown_days,
    min_business_age_months = spec.min_age,
    is_active = true
FROM (VALUES
    ('express', 'Экспресс-кредит',
     10000000::BIGINT, 500000000::BIGINT, 3, 36, 22.90,
     ARRAY['working_capital', 'cash_gap', 'other'], 'not_supported', 'annuity', NULL::SMALLINT, 6),
    ('working_capital', 'Оборотные средства',
     50000000, 5000000000, 6, 36, 19.90,
     ARRAY['working_capital'], 'optional', 'annuity', NULL, 12),
    ('equipment', 'Оборудование и транспорт',
     100000000, 10000000000, 12, 84, 18.50,
     ARRAY['equipment'], 'required', 'annuity', NULL, 12),
    ('overdraft', 'Овердрафт',
     10000000, 1000000000, 12, 12, 21.50,
     ARRAY['cash_gap', 'working_capital'], 'not_supported', 'overdraft', 60, 6)
) AS spec(code, name, min_amount, max_amount, min_term, max_term, rate,
          purposes, collateral, repayment, drawdown_days, min_age)
WHERE p.code = spec.code;

-- UUID создаётся существующим DEFAULT gen_random_uuid().
-- UNIQUE(code) и upsert исключают дубли start при повторном выполнении.
INSERT INTO credit_products
    (code, name, description, min_amount_kopecks, max_amount_kopecks,
     min_term_months, max_term_months, base_rate, purposes, collateral_mode,
     repayment_type, max_drawdown_days, min_business_age_months, is_active)
VALUES
    ('start', 'Инвестиционный кредит «Старт»', 'Кредит на запуск или расширение бизнеса',
     10000000, 300000000, 12, 36, 24.00,
     '{business_start,business_expansion}', 'required', 'annuity', NULL, 0, true)
ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    min_amount_kopecks = EXCLUDED.min_amount_kopecks,
    max_amount_kopecks = EXCLUDED.max_amount_kopecks,
    min_term_months = EXCLUDED.min_term_months,
    max_term_months = EXCLUDED.max_term_months,
    base_rate = EXCLUDED.base_rate,
    purposes = EXCLUDED.purposes,
    collateral_mode = EXCLUDED.collateral_mode,
    repayment_type = EXCLUDED.repayment_type,
    max_drawdown_days = EXCLUDED.max_drawdown_days,
    min_business_age_months = EXCLUDED.min_business_age_months,
    is_active = EXCLUDED.is_active;

-- Удаляем investment, предварительно переводя возможные ссылки на новый start.
UPDATE applications AS a
SET product_id = replacement.id
FROM credit_products AS old, credit_products AS replacement
WHERE old.code = 'investment' AND replacement.code = 'start' AND a.product_id = old.id;

UPDATE credit_decisions AS d
SET product_id = replacement.id
FROM credit_products AS old, credit_products AS replacement
WHERE old.code = 'investment' AND replacement.code = 'start' AND d.product_id = old.id;

DELETE FROM credit_products WHERE code = 'investment';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Возвращаем параметры каталога из 000003; ID четырёх продуктов сохраняются.
UPDATE credit_products AS p
SET name = old.name,
    min_amount_kopecks = old.min_amount,
    max_amount_kopecks = old.max_amount,
    min_term_months = old.min_term,
    max_term_months = old.max_term,
    base_rate = NULL,
    purposes = old.purposes::credit_purpose[],
    collateral_mode = old.collateral::collateral_mode,
    repayment_type = old.repayment::repayment_type,
    max_drawdown_days = old.drawdown_days,
    min_business_age_months = old.min_age,
    is_active = true
FROM (VALUES
    ('express', 'Экспресс-кредит',
     10000000::BIGINT, 500000000::BIGINT, 3, 36,
     ARRAY['working_capital', 'cash_gap', 'other'], 'not_supported', 'annuity', NULL::SMALLINT, 6),
    ('working_capital', 'Оборотные средства',
     50000000, 5000000000, 6, 60,
     ARRAY['working_capital'], 'optional', 'annuity', NULL, 12),
    ('equipment', 'Оборудование и транспорт',
     100000000, 10000000000, 12, 120,
     ARRAY['equipment'], 'required', 'annuity', NULL, 12),
    ('overdraft', 'Овердрафт',
     10000000, 1000000000, 12, 12,
     ARRAY['cash_gap', 'working_capital'], 'not_supported', 'overdraft', 60, 6)
) AS old(code, name, min_amount, max_amount, min_term, max_term,
         purposes, collateral, repayment, drawdown_days, min_age)
WHERE p.code = old.code;

INSERT INTO credit_products
    (code, name, description, min_amount_kopecks, max_amount_kopecks,
     min_term_months, max_term_months, base_rate, purposes, collateral_mode,
     repayment_type, max_drawdown_days, min_business_age_months, is_active)
VALUES
    ('investment', 'Инвестиционный кредит «Старт»',
     'Первоначальные вложения в запуск и развитие бизнеса, приобретение оборудования',
     10000000, 300000000, 12, 60, NULL,
     '{business_launch,equipment,business_expansion}', 'required', 'annuity', NULL, 0, true)
ON CONFLICT (code) DO NOTHING;

UPDATE applications AS a
SET product_id = replacement.id
FROM credit_products AS old, credit_products AS replacement
WHERE old.code = 'start' AND replacement.code = 'investment' AND a.product_id = old.id;

UPDATE credit_decisions AS d
SET product_id = replacement.id
FROM credit_products AS old, credit_products AS replacement
WHERE old.code = 'start' AND replacement.code = 'investment' AND d.product_id = old.id;

DELETE FROM credit_products WHERE code = 'start';

-- +goose StatementEnd
