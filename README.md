# E2B Sandbox API

A small version of the public API behind E2B sandboxes.

People sign in, create projects, share them with teammates and start sandboxes inside a project.
Every request is checked against the caller's plan, so one runaway agent cannot overload the platform,
and a developer can always see why a request was refused.

## What it does

* **Sign in with OAuth.** The service is a client of Ory Hydra and checks the JWT access token on every request.
* **Projects.** A user can own many projects, and a project can be shared with many users. Only members can see a project and its sandboxes.
* **Sandboxes.** They always live inside a project. The lifecycle is simulated: a sandbox is `running` until someone stops it.
* **Plans, limits and quotas.** Each project has a plan. The plan sets a request rate and a cap on running sandboxes. When a limit is hit, the error says which one.
* **Easy to run.** One command starts everything, another one shows it working.

## Quick start

You need Docker with Compose v2, `make`, `bash` and `curl`.

```sh
make start    # builds the image, starts the stack, applies the migrations
make demo     # walks through the API with real requests and checks every answer
```

`make start` takes about 30 seconds after the first build and ends with a summary like this:

```
==> Starting PostgreSQL, Redis, Ory Hydra and the API
  ✓ all services healthy in 26s
==> Migrations
  migrations: applied 00001_init.sql in 24ms
==> Services
  api                    Up 5 seconds (healthy)
  hydra                  Up 11 seconds (healthy)
  hydra-login-consent    Up 8 seconds (healthy)
  postgres               Up 11 seconds (healthy)
  redis                  Up 11 seconds (healthy)

E2B Sandbox API is up

  API         http://localhost:8080
  Sign in     http://localhost:8080/auth/login   (demo user: foo@bar.com / foobar)
```

Hydra's login page only exists for amd64, so on Apple Silicon it runs emulated and the very first start is slower.
These ports must be free on localhost: 5432, 6379, 8080, 4444, 4445 and 3000.
All of them are published on `127.0.0.1` only, because Hydra's admin API has no authentication.

### Try it yourself

Sign in through the browser at <http://localhost:8080/auth/login> with `foo@bar.com` and `foobar`.
The page that follows shows your access token. From a terminal, `make login` does the same login with curl and prints the token:

```sh
TOKEN=$(make -s login)

# create a project
curl -s -X POST localhost:8080/v1/projects \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"name": "My agents"}'
```
```json
{"id":"01a1a679-81be-7e67-9388-8b3bf469063e","name":"My agents","plan":"free","created_at":"2026-11-04T12:12:23.358032Z"}
```

```sh
# start a sandbox in it
curl -s -X POST localhost:8080/v1/projects/01a1a679-81be-7e67-9388-8b3bf469063e/sandboxes \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"template": "python"}'
```
```json
{"id":"01a1a679-81dc-7c71-8cc3-7fa7be0f1ed8","project_id":"01a1a679-81be-7e67-9388-8b3bf469063e","template":"python","status":"running","created_at":"2026-11-04T12:12:23.388393Z","stopped_at":null}
```

```sh
# stop it
curl -s -X DELETE localhost:8080/v1/sandboxes/01a1a679-81dc-7c71-8cc3-7fa7be0f1ed8 \
  -H "Authorization: Bearer $TOKEN"
```

Every endpoint, with examples of success and failure, is in [docs/API.md](docs/API.md).

## Tech stack

| Part | Choice | Why |
|---|---|---|
| Language | Go 1.27 | Small static binary, good concurrency, standard library HTTP router |
| Database | PostgreSQL 16 | Relational data with constraints, row locks for the quota |
| Data access | sqlc with pgx v5 | SQL stays SQL, Go code is generated and type checked |
| Migrations | goose, embedded in the binary | The image needs no extra tools |
| Cache and limits | Redis 7 with a Lua script | One atomic step per rate limit decision |
| Identity | Ory Hydra (OAuth2, JWT access tokens) | Provided by the assignment |
| Tooling | Docker Compose, make, golangci-lint, testify | One command to run, one to check |

## How it is built

The code is split in layers. Each layer only knows about the one below it.

```
   HTTP request
        |
        v
  +------------------+
  | transport/httpapi|  routes, JSON, status codes, middleware
  +------------------+
        |
        v
  +------------------+
  | usecase          |  business rules: who may do what, limits, quotas
  +------------------+
        |   talks to interfaces it defines itself
        v
  +------------------+     +------------------+
  | repository       |     | infra            |
  | Postgres, sqlc   |     | Redis, Hydra     |
  +------------------+     +------------------+
```

* `usecase` decides. It never sees HTTP, SQL or Redis, only small interfaces such as `Store` and `RateLimiter`.
* `repository` and `infra` implement those interfaces. Swapping Redis for something else touches one package.
* `internal/app` is the only place where everything is wired together.

The full picture, with the path of a request, the sign in flow and the database diagram, is in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

### Project layout

```
cmd/api                  starts the program, nothing else
internal/app             config, constants, wiring, graceful shutdown
internal/models          domain types and typed errors
internal/usecase         business rules and the interfaces they need
internal/repository      Postgres access through sqlc generated code
internal/transport/httpapi   HTTP handlers, middleware, JSON shapes
internal/infra           Postgres pool and migrations, Redis limiter, Hydra client
pkg                      small reusable helpers: ptr, convert, retry, breaker, ttlcache, idgen
db                       migrations, sqlc queries, demo fixtures
scripts                  start, login and demo scripts behind the make targets
docs                     architecture, API reference, ideas for later
```

## Everyday commands

| Command | What it does |
|---|---|
| `make start` | Build and start everything, print status |
| `make demo` | Guided tour of the API. It fails loudly if a check breaks and can be repeated |
| `make fixtures` | Load demo data: a second user, a private project, sample sandboxes. Safe to repeat |
| `make login` | Print an access token for the demo user |
| `make logs` | Follow the API logs |
| `make stop` | Stop the stack and keep the data |
| `make reset` | Stop the stack and delete the database (asks first) |
| `make test` | Unit tests with the race detector |
| `make test-integration` | Tests against real Postgres and Redis, each in its own throwaway database |
| `make lint` | golangci-lint, including security checks |
| `make sqlc` | Regenerate the database code |

`make test`, `make lint` and `make sqlc` also need Go 1.27 or newer and golangci-lint v2 on your machine.

Migrations run on every start. Demo data is separate and optional, so a new database stays empty until you run `make fixtures`.
`make demo` loads it for you.

If you restart Hydra, it forgets the OAuth client, because it keeps everything in memory.
Run `docker compose up hydra-bootstrap` to create the client again.

## Configuration

Settings come from environment variables, and `docker-compose.yml` sets them for the stack.
The database URL and the OAuth client secret have no default, so a forgotten variable stops the program at startup.

| Variable | Default | Meaning |
|---|---|---|
| `DATABASE_URL` | required | PostgreSQL connection string |
| `OAUTH_CLIENT_SECRET` | required for `serve` | Secret of the OAuth client |
| `OAUTH_CLIENT_ID` | `e2b-assignment` | Tokens must be issued to this client |
| `OAUTH_REDIRECT_URL` | `http://localhost:8080/auth/callback` | Redirect URI registered in Hydra |
| `HYDRA_ISSUER` | `http://localhost:4444` | Expected `iss` claim of every token |
| `HYDRA_AUTH_URL` | `http://localhost:4444/oauth2/auth` | Opened by the browser |
| `HYDRA_TOKEN_URL`, `HYDRA_JWKS_URL` | `http://localhost:4444/...` | Called by the service. In Docker they use the `hydra` host name |
| `REDIS_ADDR` | `localhost:6379` | Redis address |
| `HTTP_ADDR` | `:8080` | Listen address |
| `LOG_FORMAT`, `LOG_LEVEL` | `json`, `info` | Use `text` for logs that are easier to read |
| `SECURE_COOKIES` | `false` | Set to `true` behind TLS |

## Testing

* **Unit tests** cover the business rules with an in-memory store, the JWT checks against a fake provider, the cache, the circuit breaker, the retry helper, config validation and HTTP error mapping.
* **Integration tests** run against the real Postgres and Redis. They check the quota under 30 parallel requests, sharing and roles, the token bucket, and what happens when two identities claim one email. Every test gets its own database and drops it afterwards.
* **`make demo`** is the end to end test. It signs in for real and checks status codes, error codes and headers.

## Built with AI

This project was written with the help of an AI assistant (Claude Code). After that, four independent reviewer agents
looked for security, correctness, design and operations problems. They found real bugs, such as an open admin port and a
rate limiter that could retry a command twice. Each one was reproduced, fixed and covered by a test.

## More reading

* [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): layers, request flow, sign in flow, database, patterns and failure behavior
* [docs/API.md](docs/API.md): every endpoint with examples, errors, pagination, limits
* [docs/IMPROVEMENTS.md](docs/IMPROVEMENTS.md): how this design grows to millions of users and billions of sandboxes
