-- +goose Up
-- +goose StatementBegin

-- Финансовый шаг заявки (P02, P03, B05): неизменяемые принятые снимки и
-- состояние шага. Снимок модели берётся отсюда, а не из сводки компании:
-- после отправки фоновый запрос его не перезаписывает.

-- Принятый снимок финансов. Пишется только для валидных данных: ошибки
-- источника и некорректный CSV меняют состояние шага, снимка не создают.
-- rows — нормализованные месяцы в копейках (month, income_kopecks,
-- expenses_kopecks, taxes_kopecks, owner_withdrawals_kopecks, free_cash_kopecks);
-- raw — исходный ответ источника или файл CSV, наружу не отдаётся.
-- coverage — полнота окна на coverage_as_of; текущую пригодность считает сервер.
-- cycle — финансовый цикл заявки: новый открывает запрос уточнения банка.
CREATE TABLE financial_snapshots (
    id              UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    seq             BIGINT        GENERATED ALWAYS AS IDENTITY UNIQUE,
    application_id  UUID          NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    company_id      UUID          NOT NULL REFERENCES companies (id),
    cycle           INTEGER       NOT NULL CHECK (cycle > 0),
    source_type     TEXT          NOT NULL CHECK (source_type IN ('automatic', 'csv')),
    inn             TEXT          NOT NULL,
    currency        TEXT          NOT NULL CHECK (currency = 'RUB'),
    rows            JSONB         NOT NULL CHECK (jsonb_typeof(rows) = 'array' AND jsonb_array_length(rows) BETWEEN 1 AND 120),
    period_from     TEXT          NOT NULL CHECK (period_from ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
    period_to       TEXT          NOT NULL CHECK (period_to ~ '^[0-9]{4}-(0[1-9]|1[0-2])$'),
    coverage_as_of  DATE          NOT NULL,
    coverage        TEXT          NOT NULL CHECK (coverage IN ('complete', 'partial')),
    semantic_hash   CHAR(64)      NOT NULL CHECK (semantic_hash ~ '^[0-9a-f]{64}$'),
    raw             BYTEA         NOT NULL,
    raw_sha256      CHAR(64)      NOT NULL CHECK (raw_sha256 ~ '^[0-9a-f]{64}$'),
    file_name       TEXT,
    received_at     TIMESTAMPTZ   NOT NULL,
    CONSTRAINT financial_snapshots_csv_file_chk CHECK ((source_type = 'csv') = (file_name IS NOT NULL))
);

CREATE INDEX financial_snapshots_application_idx ON financial_snapshots (application_id, seq DESC);
CREATE INDEX financial_snapshots_company_idx ON financial_snapshots (company_id, seq DESC);

CREATE FUNCTION prevent_financial_snapshot_update() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'financial snapshots are immutable' USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

-- Удаление не запрещено: снимки уходят каскадом вместе с заявкой.
CREATE TRIGGER financial_snapshots_immutable
    BEFORE UPDATE ON financial_snapshots
    FOR EACH ROW EXECUTE FUNCTION prevent_financial_snapshot_update();

-- Состояние финансового шага заявки; нет строки — цикл 1, источник не запрашивался.
-- source_status — результат последнего автоматического запроса цикла; snapshot_id —
-- принятый снимок цикла (успешные автоматические данные или CSV после неуспеха).
-- no_history — клиент подтвердил отсутствие истории в резервной ветке.
-- csv_accepted — принят хотя бы один CSV: ручной маршрут до конца заявки (P03),
-- новый цикл его не снимает.
CREATE TABLE application_financials (
    application_id     UUID          PRIMARY KEY REFERENCES applications (id) ON DELETE CASCADE,
    cycle              INTEGER       NOT NULL DEFAULT 1 CHECK (cycle > 0),
    source_status      TEXT          NOT NULL DEFAULT 'not_requested'
        CHECK (source_status IN ('not_requested', 'ok', 'not_found', 'unavailable', 'invalid')),
    source_error       TEXT,
    source_checked_at  TIMESTAMPTZ,
    snapshot_id        UUID          REFERENCES financial_snapshots (id),
    no_history         BOOLEAN       NOT NULL DEFAULT false,
    csv_accepted       BOOLEAN       NOT NULL DEFAULT false,
    CONSTRAINT application_financials_history_chk CHECK (NOT (no_history AND snapshot_id IS NOT NULL))
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE application_financials;
DROP TABLE financial_snapshots;
DROP FUNCTION prevent_financial_snapshot_update();

-- +goose StatementEnd
