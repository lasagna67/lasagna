-- +goose Up
-- +goose StatementBegin

CREATE TYPE risk_grade AS ENUM ('low', 'medium', 'high', 'critical');

-- Оценка риска — вход для кредитного решения, но не само решение.
--
-- factors — вклад каждого признака в итоговый балл, например
-- [{"code": "business_age", "value": 84, "impact": -40}], для объяснимости.
CREATE TABLE scoring_results (
    id              UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id  UUID          NOT NULL REFERENCES applications (id) ON DELETE CASCADE,
    model_version   VARCHAR(50)   NOT NULL,
    score           SMALLINT      NOT NULL CHECK (score BETWEEN 0 AND 1000),
    risk_grade      risk_grade    NOT NULL,
    factors         JSONB         NOT NULL DEFAULT '[]',

    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now()
);

CREATE INDEX scoring_results_application_id_idx ON scoring_results (application_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS scoring_results;

DROP TYPE IF EXISTS risk_grade;

-- +goose StatementEnd
