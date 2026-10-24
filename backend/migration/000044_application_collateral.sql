-- +goose Up
-- +goose StatementBegin
CREATE TABLE application_collateral (
    application_id UUID PRIMARY KEY REFERENCES applications (id) ON DELETE CASCADE,
    revision BIGINT NOT NULL DEFAULT 1 CHECK (revision > 0),
    type TEXT NOT NULL CHECK (type IN ('real_estate', 'equipment', 'vehicle')),
    ownership TEXT NOT NULL CHECK (ownership IN ('existing', 'acquired')),
    description TEXT NOT NULL CHECK (btrim(description) <> ''),
    declared_value_kopecks BIGINT NOT NULL CHECK (declared_value_kopecks > 0)
);

CREATE TABLE collateral_reviews (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    application_id UUID NOT NULL REFERENCES applications (id),
    revision BIGINT NOT NULL CHECK (revision > 0),
    version BIGINT NOT NULL CHECK (version > 0),
    reviewer UUID NOT NULL REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    collateral_hash TEXT NOT NULL CHECK (collateral_hash ~ '^[0-9a-f]{64}$'),
    collateral_revision BIGINT NOT NULL CHECK (collateral_revision > 0),
    result TEXT NOT NULL CHECK (result IN ('accepted', 'needs_info', 'rejected')),
    accepted_value_kopecks BIGINT CHECK (accepted_value_kopecks > 0),
    comment TEXT NOT NULL CHECK (btrim(comment) <> ''),
    public_comment TEXT,
    CHECK (result <> 'accepted' OR accepted_value_kopecks IS NOT NULL),
    UNIQUE (application_id, version)
);

CREATE FUNCTION prevent_collateral_review_change() RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'collateral reviews are immutable' USING ERRCODE = '55000';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER collateral_reviews_immutable
    BEFORE UPDATE OR DELETE ON collateral_reviews
    FOR EACH ROW EXECUTE FUNCTION prevent_collateral_review_change();
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE collateral_reviews;
DROP FUNCTION prevent_collateral_review_change();
DROP TABLE application_collateral;
-- +goose StatementEnd
