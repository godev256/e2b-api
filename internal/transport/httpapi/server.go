package httpapi

import (
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/buildoshi/e2b-api/internal/models"
)

// Config holds the transport tunables.
type Config struct {
	RequestTimeout time.Duration
	MaxBodyBytes   int64
	SecureCookies  bool
}

// Deps are the collaborators of the HTTP layer.
type Deps struct {
	Auth      Authenticator
	Projects  ProjectService
	Sandboxes SandboxService
	OAuth     OAuthFlow
	Readiness map[string]ReadinessCheck
	// Draining flips to true when shutdown starts, so /readyz tells load
	// balancers to stop sending traffic while in-flight requests finish.
	Draining *atomic.Bool
	Log      *slog.Logger
}

// Server wires routes to handlers.
type Server struct {
	Deps
	cfg Config
}

// NewHandler builds the complete HTTP handler, middleware included.
func NewHandler(cfg Config, deps Deps) http.Handler {
	s := &Server{Deps: deps, cfg: cfg}

	mux := http.NewServeMux()

	// Operations and the OAuth flow need no bearer token.
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /readyz", s.handleReady)
	mux.HandleFunc("GET /auth/login", s.handleLogin)
	mux.HandleFunc("GET /auth/callback", s.handleCallback)

	// The public API.
	mux.Handle("POST /v1/projects", s.authenticated(s.handleCreateProject))
	mux.Handle("GET /v1/projects", s.authenticated(s.handleListProjects))
	mux.Handle("POST /v1/projects/{id}/members", s.authenticated(s.handleAddMember))
	mux.Handle("GET /v1/projects/{id}/members", s.authenticated(s.handleListMembers))
	mux.Handle("POST /v1/projects/{id}/sandboxes", s.authenticated(s.handleCreateSandbox))
	mux.Handle("GET /v1/projects/{id}/sandboxes", s.authenticated(s.handleListSandboxes))
	mux.Handle("DELETE /v1/sandboxes/{id}", s.authenticated(s.handleStopSandbox))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.writeError(w, r, models.ErrNotFound)
	})

	return s.chain(recordRoute(mux))
}
