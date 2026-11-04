package app

import "time"

// Every tunable lives here, named and documented, so there are no magic
// numbers elsewhere. Operational settings that differ per environment come
// from the environment instead (see config.go).
const (
	// HTTP server. The header and body timeouts protect against slow clients.
	serverReadHeaderTimeout = 5 * time.Second
	serverReadTimeout       = 15 * time.Second
	serverWriteTimeout      = 30 * time.Second
	serverIdleTimeout       = 60 * time.Second
	shutdownTimeout         = 20 * time.Second

	// requestTimeout caps the work done for one request. It must stay below
	// serverWriteTimeout so the handler fails before the connection is cut.
	requestTimeout = 10 * time.Second
	maxBodyBytes   = 1 << 20

	// Postgres pool.
	dbMaxConns         = 20
	dbMinConns         = 2
	dbMaxConnLifetime  = 30 * time.Minute
	dbMaxConnIdleTime  = 5 * time.Minute
	dbHealthCheckEvery = 30 * time.Second
	dbStatementTimeout = 5 * time.Second
	dbLockTimeout      = 3 * time.Second

	// Migrations and fixtures may run DDL or backfills that outlast a request,
	// so their pool has no statement timeout (zero disables it in Postgres).
	maintenanceStatementTimeout = 0

	// Redis. Short, because the rate limiter is on the hot path.
	redisDialTimeout  = 1 * time.Second
	redisReadTimeout  = 200 * time.Millisecond
	redisWriteTimeout = 200 * time.Millisecond
	redisPoolSize     = 20

	// After breakerThreshold consecutive Redis failures the limiter stops
	// calling Redis for breakerCooldown and the API serves unlimited meanwhile.
	breakerThreshold = 5
	breakerCooldown  = 10 * time.Second

	// Calls to the OAuth provider.
	hydraHTTPTimeout   = 5 * time.Second
	jwksTTL            = 10 * time.Minute
	jwksMinRefresh     = 5 * time.Second
	jwksRetryAttempts  = 3
	jwksRetryBaseDelay = 100 * time.Millisecond
	jwksRetryMaxDelay  = 1 * time.Second
	jwksFetchTimeout   = 5 * time.Second
	tokenLeeway        = 30 * time.Second

	// healthcheck is the in-container probe used by Docker.
	healthcheckTimeout = 3 * time.Second
	healthcheckPath    = "/healthz"
	defaultPort        = ":8080"

	oauthScopes = "openid offline"
)
