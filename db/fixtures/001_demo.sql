{{- /* foo's id is looked up, never assumed: if foo signed in before the fixtures
       were loaded, the row already exists with a generated id. */ -}}
{{ define "foo" }}(SELECT id FROM users WHERE issuer = {{ quote .Issuer }} AND subject = 'foo@bar.com'){{ end }}
-- Demo data for reviewers: `make fixtures`. Safe to run repeatedly, in any order
-- relative to the first login.
--
-- foo@bar.com is the Hydra demo account. It is created here with the same
-- identity (issuer, subject) that the first real login resolves to, so the login
-- attaches to these projects instead of creating a second user.
--
-- bob@example.com has no Hydra account. He exists to show sharing: add him to
-- a project and he appears in its member list. He is not in "Bob Private".

INSERT INTO users (id, issuer, subject, email) VALUES
    ('0192f000-0000-7000-8000-000000000001', {{ quote .Issuer }}, 'foo@bar.com',     'foo@bar.com'),
    ('0192f000-0000-7000-8000-000000000002', {{ quote .Issuer }}, 'bob@example.com', 'bob@example.com')
ON CONFLICT (issuer, subject) DO NOTHING;

INSERT INTO projects (id, name, plan_code, created_by) VALUES
    ('0192f000-0000-7000-8000-0000000000a1', 'Acme Agents', 'free', {{ template "foo" . }}),
    ('0192f000-0000-7000-8000-0000000000a2', 'Bob Private', 'free', '0192f000-0000-7000-8000-000000000002')
ON CONFLICT (id) DO NOTHING;

INSERT INTO project_members (project_id, user_id, role, added_by) VALUES
    ('0192f000-0000-7000-8000-0000000000a1', {{ template "foo" . }}, 'owner', {{ template "foo" . }}),
    ('0192f000-0000-7000-8000-0000000000a2', '0192f000-0000-7000-8000-000000000002', 'owner', '0192f000-0000-7000-8000-000000000002')
ON CONFLICT (project_id, user_id) DO NOTHING;

INSERT INTO sandboxes (id, project_id, created_by, template, status, stopped_at) VALUES
    ('0192f000-0000-7000-8000-0000000000b1', '0192f000-0000-7000-8000-0000000000a1', {{ template "foo" . }}, 'base',   'running', NULL),
    ('0192f000-0000-7000-8000-0000000000b2', '0192f000-0000-7000-8000-0000000000a1', {{ template "foo" . }}, 'python', 'stopped', now()),
    ('0192f000-0000-7000-8000-0000000000b3', '0192f000-0000-7000-8000-0000000000a2', '0192f000-0000-7000-8000-000000000002', 'base', 'running', NULL)
ON CONFLICT (id) DO NOTHING;
