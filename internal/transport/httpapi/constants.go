package httpapi

import "time"

const (
	// Routes and headers.
	headerRequestID  = "X-Request-Id"
	headerRetryAfter = "Retry-After"
	headerRateLimit  = "X-RateLimit-Limit"
	headerRateRemain = "X-RateLimit-Remaining"
	headerRateReset  = "X-RateLimit-Reset"

	bearerPrefix = "Bearer "

	// Input handling.
	maxRequestIDLength = 64
	pageLimitParam     = "limit"
	pageCursorParam    = "cursor"

	// OAuth cookies. They live only for the duration of one login.
	cookieOAuthState    = "oauth_state"
	cookieOAuthVerifier = "oauth_verifier"
	cookiePath          = "/auth"
	cookieTTL           = 10 * time.Minute
	oauthStateBytes     = 24

	// Postgres SQLSTATE values the transport knows how to explain.
	sqlStateLockNotAvailable = "55P03"
	sqlStateQueryCanceled    = "57014"
	sqlStateUniqueViolation  = "23505"
	sqlStateDataExceptionCls = "22"

	// busyRetryAfter is what a client is told to wait after a lock or statement timeout.
	busyRetryAfter = "1"

	// readinessTimeout bounds each dependency check of /readyz.
	readinessTimeout = 2 * time.Second
)

// Machine readable error codes returned in the `error.code` field.
const (
	codeForbidden        = "forbidden"
	codeBusy             = "busy"
	codeConflict         = "conflict"
	codeProjectQuota     = "project_quota_exceeded"
	codeInvalidRequest   = "invalid_request"
	codeUnauthenticated  = "unauthenticated"
	codeNotFound         = "not_found"
	codeUserNotFound     = "user_not_registered"
	codeRateLimited      = "rate_limit_exceeded"
	codeSandboxQuota     = "sandbox_quota_exceeded"
	codeInternal         = "internal_error"
	codeUnavailable      = "service_unavailable"
	codeRequestTooLarge  = "request_too_large"
	codeUnsupportedMedia = "unsupported_media_type"
)
