-- +goose Up
-- +goose StatementBegin

-- Общий триггер для updated_at, используется всеми таблицами ниже.
CREATE FUNCTION set_updated_at() RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Доп
CREATE TYPE user_status AS ENUM ('active', 'banned', 'deleted');
CREATE TYPE user_gender AS ENUM ('male', 'female');

-- client — представитель бизнеса, остальные — сотрудники банка.
CREATE TYPE user_role   AS ENUM ('client', 'underwriter', 'admin');

-- 
CREATE TABLE users (
    id            UUID              PRIMARY KEY DEFAULT gen_random_uuid(),
    role          user_role         NOT NULL DEFAULT 'client',
    status        user_status       NOT NULL DEFAULT 'active',

    email         VARCHAR(255)      NOT NULL,
    phone         VARCHAR(16)       UNIQUE,
    password_hash TEXT              NOT NULL,

    first_name    VARCHAR(100),
    last_name     VARCHAR(100),
    middle_name   VARCHAR(100),

    gender        user_gender,
    birth_date    DATE,

    created_at    TIMESTAMPTZ       NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ       NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX users_email_uidx ON users (lower(email));

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS users;

DROP TYPE IF EXISTS user_role;
DROP TYPE IF EXISTS user_gender;
DROP TYPE IF EXISTS user_status;

DROP FUNCTION IF EXISTS set_updated_at();

-- +goose StatementEnd
