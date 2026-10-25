-- +goose Up
-- +goose StatementBegin

-- Конвейер проверок после отправки (B03): validation → enrichment → scoring →
-- manual_review либо rejected по stop-правилу. Этапы связаны событиями Kafka;
-- событие пишется в outbox в одной транзакции с переходом заявки.

-- processing_run_id — текущий запуск конвейера. Новый при каждой отправке и
-- повторной отправке; результаты чужого запуска отбрасываются, а не перезаписывают
-- заявку. Сверяется вместо revision: записи одного запуска не конфликтуют друг с
-- другом только потому, что предыдущая увеличила revision.
ALTER TABLE applications ADD COLUMN processing_run_id UUID;

UPDATE applications SET processing_run_id = gen_random_uuid() WHERE submitted_at IS NOT NULL;

ALTER TABLE applications ADD CONSTRAINT applications_processing_run_chk
    CHECK (submitted_at IS NULL OR processing_run_id IS NOT NULL);

-- Transactional outbox: сообщение пишется в транзакции бизнес-изменения, relay
-- публикует его в Kafka и ставит published_at. Падение Kafka или процесса после
-- коммита не теряет событие — оно уйдёт при следующем проходе relay.
-- key — ключ партиции Kafka (id заявки): события одной заявки идут по порядку.
CREATE TABLE outbox (
    id            BIGINT        GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    topic         TEXT          NOT NULL CHECK (btrim(topic) <> ''),
    key           TEXT          NOT NULL,
    payload       JSONB         NOT NULL,
    created_at    TIMESTAMPTZ   NOT NULL DEFAULT now(),
    published_at  TIMESTAMPTZ
);

CREATE INDEX outbox_unpublished_idx ON outbox (id) WHERE published_at IS NULL;

-- Заявки, которые до конвейера застряли в submitted (сбой прежнего синхронного
-- запуска), запускаются заново. Топик совпадает с pipelinedomain.TopicSubmitted.
INSERT INTO outbox (topic, key, payload)
SELECT 'lasagna.applications.submitted', id::text,
       jsonb_build_object('application_id', id, 'run_id', processing_run_id)
FROM applications WHERE status = 'submitted';

-- Снимок источника относится к запуску конвейера, в том числе повтор сотрудником
-- (retry-checks) — к текущему запуску заявки. NULL — снимки до конвейера.
ALTER TABLE external_checks ADD COLUMN processing_run_id UUID;

-- Результат модели (P05, P11, P12). score и risk_grade есть только у завершённой
-- балльной модели (cashflow, overdraft); у проекта — project_result в report и
-- null-балл; у insufficient_data, not_configured, failed, skipped — null, а не 0.
-- model_version отсутствует только у not_configured и skipped.
-- recommendation — маршрут модели: manual_review, auto_approved, auto_rejected;
-- report — полный отчёт расчёта для сотрудника.
ALTER TABLE scoring_results
    ADD COLUMN processing_run_id  UUID,
    ADD COLUMN kind               TEXT     CHECK (kind IN ('cashflow', 'project', 'overdraft')),
    ADD COLUMN status             TEXT     NOT NULL DEFAULT 'completed'
        CHECK (status IN ('completed', 'insufficient_data', 'not_configured', 'failed', 'skipped')),
    ADD COLUMN data_revision      INTEGER  CHECK (data_revision > 0),
    ADD COLUMN recommendation     TEXT     CHECK (recommendation IN ('manual_review', 'auto_approved', 'auto_rejected')),
    ADD COLUMN can_publish        BOOLEAN  NOT NULL DEFAULT false,
    ADD COLUMN positive_blockers  TEXT[]   NOT NULL DEFAULT '{}',
    ADD COLUMN report             JSONB,
    ALTER COLUMN model_version DROP NOT NULL,
    ALTER COLUMN score DROP NOT NULL,
    ALTER COLUMN risk_grade DROP NOT NULL,
    ADD CONSTRAINT scoring_results_score_chk CHECK (
        CASE
            WHEN status = 'completed' AND kind = 'project' THEN score IS NULL AND risk_grade IS NULL
            WHEN status = 'completed' THEN score IS NOT NULL AND risk_grade IS NOT NULL
            ELSE score IS NULL AND risk_grade IS NULL
        END
    ),
    ADD CONSTRAINT scoring_results_model_chk CHECK (
        model_version IS NOT NULL OR status IN ('not_configured', 'skipped')
    );

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DELETE FROM scoring_results WHERE score IS NULL OR risk_grade IS NULL OR model_version IS NULL;

ALTER TABLE scoring_results
    DROP CONSTRAINT scoring_results_model_chk,
    DROP CONSTRAINT scoring_results_score_chk,
    ALTER COLUMN risk_grade SET NOT NULL,
    ALTER COLUMN score SET NOT NULL,
    ALTER COLUMN model_version SET NOT NULL,
    DROP COLUMN report,
    DROP COLUMN positive_blockers,
    DROP COLUMN can_publish,
    DROP COLUMN recommendation,
    DROP COLUMN data_revision,
    DROP COLUMN status,
    DROP COLUMN kind,
    DROP COLUMN processing_run_id;

ALTER TABLE external_checks DROP COLUMN processing_run_id;

DROP TABLE outbox;

ALTER TABLE applications
    DROP CONSTRAINT applications_processing_run_chk,
    DROP COLUMN processing_run_id;

-- +goose StatementEnd
