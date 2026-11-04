# API reference

Base URL when running locally: `http://localhost:8080`.

All examples assume you have a token in a shell variable:

```sh
TOKEN=$(make -s login)
```

Send it in the `Authorization` header of every `/v1` call. All bodies and answers are JSON.

## Contents

* [Signing in](#signing-in)
* [Projects](#projects)
* [Members](#members)
* [Sandboxes](#sandboxes)
* [Health checks](#health-checks)
* [Lists and pagination](#lists-and-pagination)
* [Limits and quotas](#limits-and-quotas)
* [Errors](#errors)

## Signing in

There is no password endpoint. People sign in with OAuth, and the first sign in creates their account.

### `GET /auth/login`

Starts the flow. It redirects the browser to Hydra. Open it in a browser:

```
http://localhost:8080/auth/login
```

Log in with the demo user `foo@bar.com` and password `foobar`, then allow access.

### `GET /auth/callback`

Hydra sends the browser back here with a code. The service checks the `state`, exchanges the code and shows the access token.
A browser gets a small page with the token and a ready curl command. Any other client (for example curl) gets JSON:

```json
{
  "access_token": "eyJhbGciOiJSUzI1NiIs...",
  "token_type": "Bearer",
  "expires_in": 3599,
  "user": { "id": "0192f000-0000-7000-8000-000000000001", "email": "foo@bar.com" }
}
```

Tokens last one hour. `make login` runs the whole flow with curl and prints just the token.

## Projects

A project is the unit of ownership. It has a plan, and every user who belongs to it can see its sandboxes.

### `POST /v1/projects`

Creates a project. The caller becomes its owner. New projects start on the `free` plan.
One user can own at most 10 projects.

```sh
curl -s -X POST localhost:8080/v1/projects \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name": "My agents"}'
```

`201 Created`

```json
{
  "id": "01a1a679-81be-7e67-9388-8b3bf469063e",
  "name": "My agents",
  "plan": "free",
  "created_at": "2026-11-04T12:12:23.358032Z"
}
```

The name must be 1 to 100 characters. Control characters and text direction overrides are refused with `400`.

### `GET /v1/projects`

Lists the projects the caller belongs to, as owner or as member.

```sh
curl -s localhost:8080/v1/projects -H "Authorization: Bearer $TOKEN"
```

`200 OK`

```json
{
  "items": [
    { "id": "0192f000-0000-7000-8000-0000000000a1", "name": "Acme Agents", "plan": "free", "created_at": "2026-11-04T12:12:22.990822Z" },
    { "id": "01a1a679-81be-7e67-9388-8b3bf469063e", "name": "My agents", "plan": "free", "created_at": "2026-11-04T12:12:23.358032Z" }
  ],
  "next_cursor": null
}
```

## Members

### `POST /v1/projects/{id}/members`

Adds a person to a project. From then on, the project and all its sandboxes are theirs too.
Only the project's owners can do this, and the person must have signed in at least once.

```sh
curl -s -X POST localhost:8080/v1/projects/$PROJECT_ID/members \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"email": "bob@example.com"}'
```

`201 Created` for a new member, `200 OK` if the person was already in the project (the call is safe to repeat).

```json
{
  "user_id": "0192f000-0000-7000-8000-000000000002",
  "email": "bob@example.com",
  "role": "member",
  "added_at": "2026-11-04T12:12:23.380334Z"
}
```

Possible failures:

| Status | Code | When |
|---|---|---|
| 400 | `invalid_request` | The email is not valid |
| 403 | `forbidden` | The caller is a member but not an owner |
| 404 | `not_found` | The project does not exist, or the caller is not in it |
| 404 | `user_not_registered` | Nobody with that email has signed in yet |

### `GET /v1/projects/{id}/members`

Lists who shares the project. Supports [pagination](#lists-and-pagination).

```json
{
  "items": [
    { "user_id": "0192f000-0000-7000-8000-000000000001", "email": "foo@bar.com", "role": "owner", "added_at": "2026-11-04T12:12:23.358032Z" },
    { "user_id": "0192f000-0000-7000-8000-000000000002", "email": "bob@example.com", "role": "member", "added_at": "2026-11-04T12:12:23.380334Z" }
  ],
  "next_cursor": null
}
```

## Sandboxes

The lifecycle is simulated. A new sandbox is `running` right away, and it stays that way until it is stopped.

### `POST /v1/projects/{id}/sandboxes`

Starts a sandbox. The body is optional. The `template` defaults to `base` and may contain lowercase letters, digits, dots, dashes and underscores.

```sh
curl -s -X POST localhost:8080/v1/projects/$PROJECT_ID/sandboxes \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"template": "python"}'
```

`201 Created`

```json
{
  "id": "01a1a679-81dc-7c71-8cc3-7fa7be0f1ed8",
  "project_id": "01a1a679-81be-7e67-9388-8b3bf469063e",
  "template": "python",
  "status": "running",
  "created_at": "2026-11-04T12:12:23.388393Z",
  "stopped_at": null
}
```

When the project already runs as many sandboxes as its plan allows, the answer is `429`:

```json
{
  "error": {
    "code": "sandbox_quota_exceeded",
    "message": "the project reached the running sandbox limit of its plan, stop a sandbox or upgrade the plan",
    "request_id": "3a4ec091-e46d-422a-8174-4437e8ce06d6",
    "details": { "limit": 3, "running": 3 }
  }
}
```

### `GET /v1/projects/{id}/sandboxes`

Lists all sandboxes of the project, stopped ones included, oldest first. Supports [pagination](#lists-and-pagination).

```sh
curl -s "localhost:8080/v1/projects/$PROJECT_ID/sandboxes?limit=2" -H "Authorization: Bearer $TOKEN"
```

### `DELETE /v1/sandboxes/{id}`

Stops a sandbox. The path has no project, so the service finds the sandbox first and then checks that the caller belongs to its project.

```sh
curl -s -X DELETE localhost:8080/v1/sandboxes/$SANDBOX_ID -H "Authorization: Bearer $TOKEN"
```

`200 OK`. The record is kept and its status changes:

```json
{
  "id": "01a1a679-81dc-7c71-8cc3-7fa7be0f1ed8",
  "project_id": "01a1a679-81be-7e67-9388-8b3bf469063e",
  "template": "python",
  "status": "stopped",
  "created_at": "2026-11-04T12:12:23.388393Z",
  "stopped_at": "2026-11-04T12:12:23.406423Z"
}
```

Stopping a sandbox that is already stopped is not an error. You get the same `200` answer.

## Health checks

No token is needed.

| Path | Meaning |
|---|---|
| `GET /healthz` | The process is alive. The Docker healthcheck uses it |
| `GET /readyz` | The service can do its job: Postgres and Redis answer, and Hydra's keys were loaded |

```json
{"checks":{"hydra":"ok","postgres":"ok","redis":"ok"},"status":"ok"}
```

`/readyz` returns `503` when a check fails, and also while the service is shutting down (`{"status":"draining"}`).

## Lists and pagination

Lists use a cursor. Ask for a page size with `limit` (default 50, maximum 200), and pass the previous `next_cursor` to get the next page.

```sh
curl -s "localhost:8080/v1/projects/$PROJECT_ID/sandboxes?limit=50" -H "Authorization: Bearer $TOKEN"
curl -s "localhost:8080/v1/projects/$PROJECT_ID/sandboxes?limit=50&cursor=01a1a679-81dc-7c71-8cc3-7fa7be0f1ed8" -H "Authorization: Bearer $TOKEN"
```

When `next_cursor` is `null`, you have reached the end. A malformed `limit` or `cursor` gives `400`.

## Limits and quotas

Every project has a plan, and the plan sets two limits.

| Plan | Requests per second | Burst | Running sandboxes |
|---|---|---|---|
| `free` (every new project) | 5 | 10 | 3 |
| `pro` | 50 | 100 | 100 |

* **Rate limit.** Requests that use a project (its sandboxes, its members) share the project's bucket. The bucket holds `burst` requests and refills at the per-second speed.
  When it is empty you get `429` with `Retry-After`.
* **Sandbox quota.** The number of running sandboxes of a project cannot pass the plan's limit. Stop a sandbox to free a slot.
* **Project quota.** One user can own at most 10 projects.
* **Unknown resources.** Asking again and again for projects or sandboxes that do not exist or are not yours is limited too. This prevents id guessing.

A rate limited answer looks like this:

```
HTTP/1.1 429 Too Many Requests
Retry-After: 1
X-RateLimit-Limit: 10
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 2
```
```json
{
  "error": {
    "code": "rate_limit_exceeded",
    "message": "too many requests for this plan, retry after the delay in the Retry-After header",
    "request_id": "6dd77a1e-1c7f-4a9e-bd0a-2f3a5e0f2f10",
    "details": { "limit": 10, "retry_after_seconds": 1 }
  }
}
```

`X-RateLimit-Reset` is the number of seconds until the bucket is full again.

Both the rate limit and the sandbox quota use status `429`. Look at `error.code` to tell them apart.
Only the rate limit has a `Retry-After`, because waiting does not free a sandbox slot.

## Errors

Every error has the same shape. The `code` is stable and meant for programs, the `message` is meant for people.
The `request_id` is also sent in the `X-Request-Id` header and appears in the server logs, so quote it when you report a problem.

```json
{
  "error": {
    "code": "not_found",
    "message": "resource not found",
    "request_id": "9215cd97-0741-4afe-848e-1413bc457308"
  }
}
```

| Status | Code | Meaning |
|---|---|---|
| 400 | `invalid_request` | Bad JSON, unknown field, invalid value, malformed `limit` or `cursor` |
| 401 | `unauthenticated` | No token, a broken token, an expired token, or a token meant for another OAuth client |
| 403 | `forbidden` | You are a member but the action needs an owner |
| 404 | `not_found` | The resource does not exist, or you may not see it. The two cases look the same on purpose |
| 404 | `user_not_registered` | The person you want to add has not signed in yet |
| 409 | `conflict` | The request clashes with existing data |
| 413 | `request_too_large` | The body is larger than 1 MiB |
| 415 | `unsupported_media_type` | Send `Content-Type: application/json` |
| 429 | `rate_limit_exceeded` | Too many requests, see `Retry-After` |
| 429 | `sandbox_quota_exceeded` | The project runs as many sandboxes as its plan allows |
| 429 | `project_quota_exceeded` | You own the maximum number of projects |
| 500 | `internal_error` | A bug. The details are in the server log under the `request_id`, never in the answer |
| 503 | `busy` | A lock or query timed out. Try again after `Retry-After` |
| 503 | `service_unavailable` | The service cannot check tokens right now because Hydra is unreachable |
