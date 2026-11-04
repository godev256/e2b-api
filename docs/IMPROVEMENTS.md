# Improvements and scaling notes

The implementation is small on purpose. This is how the architecture would grow to millions of
users and billions of sandboxes, and what was left out.

## Data layer

**Shard by `project_id`.** Every hot query already filters by project (`sandboxes (project_id, id)`,
the quota count, the rate limit key). Routing by `hash(project_id)` to a shard keeps a project's
sandboxes, members and quota decisions on one node, so the `FOR NO KEY UPDATE` quota
transaction stays local and no distributed transaction is needed. Options, in order of effort:
native partitioning of `sandboxes` by hash of `project_id`, then Citus (distribute on `project_id`), then
application level sharding with a small directory table `project_shards`.

Sharding by project has three consequences that the current schema does not handle yet, and each has a known answer.

* **Routing a request that carries only a sandbox id.** `DELETE /v1/sandboxes/{id}` has no project in the path, so there is
  nothing to hash. The simplest fix is to put the shard in the id: an id such as `<shard>-<uuidv7>` routes without a lookup.
  The alternative is a small global `sandbox_directory (sandbox_id, project_id)` table, which is one more write per sandbox
  and one more read per request, but keeps ids opaque. Either way the lookup then continues on the right shard.
* **Partitioning needs the partition key in every unique constraint.** With native partitioning by `project_id`, the primary key of
  `sandboxes` must become `(project_id, id)`. Lookup by `id` alone would scan every partition, which is exactly why the id
  must carry its routing information.
* **The "which projects can this user see" direction fans out.** Members are stored with their project, so listing a user's projects
  would ask every shard. Keep a denormalized `user_projects (user_id, project_id)` table, keyed by user and written in the same
  workflow as membership changes (or through an outbox). `users` and email lookup (`GetUserByEmail`) are global by nature,
  so they live in one small replicated table or service, separate from the sharded sandbox data.

**Keep the table small.** Billions of rows are mostly stopped sandboxes. Partition `sandboxes` by time
as well, detach old partitions to cold storage, and keep the running set in a separate narrow table or
rely on the partial index. The `(project_id, id)` keyset pagination is already offset free.
Deleting a project with a very large history must not be one `ON DELETE CASCADE` transaction. Mark it deleted and purge in batches.

**Quota without counting.** `count(*) WHERE status = 'running'` is cheap with the partial index, but for projects
with tens of thousands of running sandboxes a maintained counter (`running_sandboxes` on the project row, updated in the
same transaction) turns the check into one row read. Reconcile the counter with the orchestrator, because a real sandbox can die without the API noticing.

**Account level quotas.** Limits are per project and a user can own 10 projects, so the worst case per user is bounded but not small.
A per user ceiling across all projects, or billing level limits per organization, closes that gap. The tenancy model would then gain an
organization above projects.

**Membership reads.** `GetProjectAccess` runs on every request. Cache `(project, user) -> role` in Redis with a
short TTL and invalidate on member changes through pub/sub. Add read replicas for list endpoints.

**Connections.** PgBouncer in transaction mode in front of Postgres once the service runs on many instances.

## Rate limiting

* The Lua token bucket is already shard friendly (hash tagged keys). Move to Redis Cluster or one Redis per region.
* One hot project is one hot Redis key. Hierarchical limits help: a small local bucket per API instance that refills from the shared bucket in chunks.
* Add a pre authentication limit per IP to protect the token verification path and the OAuth endpoints, which are unmetered today.
* Return `X-RateLimit-*` headers on successful responses too, so clients can pace themselves.
* Separate limits per operation class (create sandbox costs more than list).
* Redis outage policy is fail open behind a circuit breaker today. For abuse sensitive routes, fall back to a coarse in process limiter.
  A load balancer health check should not depend on Redis, and `/readyz` should split "ready to serve" from "all dependencies healthy".

## Sandbox lifecycle

The lifecycle is simulated. The real one needs an orchestrator. Write the intent (`starting`, `stopping`) and an
outbox row in the same transaction, a worker drives the sandbox to the target state and reports back, with
reconciliation for stuck states. Add idempotency keys for `POST` so a retried create does not start two sandboxes.
Add sandbox timeouts and automatic stop. Usage events (start, stop, duration) feed billing and need their own durable stream.

## Auth

* Machine API keys (hashed, scoped to a project) next to OAuth JWTs, since agents do not log in with a browser.
* Refresh token handling and sessions for the callback page, token revocation list.
* Roles beyond `owner` and `member`, member removal and leaving a project, ownership transfer, and an audit log of membership changes.
  Adding members by email lets an owner learn whether an address is registered, so a real product would use pending invitations that the invitee accepts.
* Require `email_verified` before using an email for invites, and give the API its own audience in Hydra so `aud` can be checked as well as `client_id`.

## Operations

* Metrics (Prometheus) and traces (OpenTelemetry): request rate, latency, 429 rate per plan, quota refusals, JWKS refetches, Redis breaker state.
* Structured logs already carry a request id. Propagate it to downstream calls and add the user and project ids to the access log.
* Run migrations as a separate release step with a backward compatible expand and contract discipline.
* Multi region: data residency decides where a project lives, and the project directory and the global tables above need a replication story.
* CI: `make check`, integration tests against service containers, `sqlc generate` diff check, container image scan, pinned image digests.
* Load test the quota path with many projects and with one hot project to validate the row lock approach.
