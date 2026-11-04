-- +goose Up
-- +goose StatementBegin

-- plans: reference data. A plan is a named bundle of limits, so changing a
-- limit is one UPDATE here instead of a change on every project row.
CREATE TABLE plans (
    code                    text PRIMARY KEY,
    name                    text    NOT NULL,
    max_requests_per_second integer NOT NULL CHECK (max_requests_per_second > 0),
    burst                   integer NOT NULL CHECK (burst > 0),
    max_running_sandboxes   integer NOT NULL CHECK (max_running_sandboxes >= 0)
);

INSERT INTO plans (code, name, max_requests_per_second, burst, max_running_sandboxes) VALUES
    ('free', 'Free', 5, 10, 3),
    ('pro',  'Pro',  50, 100, 100);

-- users: one row per identity at the OAuth provider. The identity is the pair
-- (issuer, subject). Email is a mutable attribute and never a key.
CREATE TABLE users (
    id         uuid PRIMARY KEY,
    issuer     text        NOT NULL,
    subject    text        NOT NULL,
    email      text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_identity_key UNIQUE (issuer, subject),
    CONSTRAINT users_email_lowercase CHECK (email IS NULL OR email = lower(email))
);
CREATE UNIQUE INDEX users_email_key ON users (email) WHERE email IS NOT NULL;

-- projects: the tenancy unit. Everything a customer owns hangs off a project.
CREATE TABLE projects (
    id         uuid PRIMARY KEY,
    name       text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    plan_code  text        NOT NULL REFERENCES plans (code),
    created_by uuid        NOT NULL REFERENCES users (id),
    created_at timestamptz NOT NULL DEFAULT now()
);

-- project_members: many to many link between users and projects.
CREATE TYPE project_role AS ENUM ('owner', 'member');

CREATE TABLE project_members (
    project_id uuid         NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    user_id    uuid         NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    role       project_role NOT NULL DEFAULT 'member',
    added_by   uuid         NOT NULL REFERENCES users (id),
    added_at   timestamptz  NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);
-- The primary key serves "who is in this project". This one serves
-- "which projects does this user see".
CREATE INDEX project_members_user_id_idx ON project_members (user_id, project_id);

-- sandboxes: the records the public API manages. project_id is the shard key.
CREATE TYPE sandbox_status AS ENUM ('running', 'stopped');

CREATE TABLE sandboxes (
    id         uuid PRIMARY KEY,
    project_id uuid           NOT NULL REFERENCES projects (id) ON DELETE CASCADE,
    created_by uuid           NOT NULL REFERENCES users (id),
    template   text           NOT NULL DEFAULT 'base',
    status     sandbox_status NOT NULL DEFAULT 'running',
    created_at timestamptz    NOT NULL DEFAULT now(),
    stopped_at timestamptz,
    CONSTRAINT sandboxes_stopped_at_matches_status
        CHECK ((status = 'stopped') = (stopped_at IS NOT NULL))
);
-- Keyset pagination: list one project's sandboxes in id order.
CREATE INDEX sandboxes_project_id_id_idx ON sandboxes (project_id, id);
-- Quota check: count only running sandboxes, a tiny slice of the table.
CREATE INDEX sandboxes_running_project_id_idx ON sandboxes (project_id) WHERE status = 'running';

-- +goose StatementEnd

-- +goose Down
DROP TABLE sandboxes;
DROP TYPE sandbox_status;
DROP TABLE project_members;
DROP TYPE project_role;
DROP TABLE projects;
DROP TABLE users;
DROP TABLE plans;
