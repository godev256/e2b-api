// Package app wires every dependency together and runs the process commands.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	"github.com/buildoshi/e2b-api/internal/infra/hydra"
	"github.com/buildoshi/e2b-api/internal/infra/logging"
	"github.com/buildoshi/e2b-api/internal/infra/postgres"
	"github.com/buildoshi/e2b-api/internal/infra/ratelimit"
	"github.com/buildoshi/e2b-api/internal/repository"
	transport "github.com/buildoshi/e2b-api/internal/transport/httpapi"
	"github.com/buildoshi/e2b-api/internal/usecase"
	"github.com/buildoshi/e2b-api/pkg/breaker"
	"github.com/buildoshi/e2b-api/pkg/retry"
)

// Commands understood by Run.
const (
	CommandServe       = "serve"
	CommandMigrate     = "migrate"
	CommandFixtures    = "fixtures"
	CommandHealthcheck = "healthcheck"
)

// Run executes one process command. An empty command means serve.
func Run(ctx context.Context, command string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log := logging.New(os.Stderr, cfg.LogFormat, cfg.LogLevel)

	switch command {
	case "", CommandServe:
		return serve(ctx, cfg, log)
	case CommandMigrate:
		return migrate(ctx, cfg)
	case CommandFixtures:
		return fixtures(ctx, cfg)
	case CommandHealthcheck:
		return healthcheck(ctx, cfg)
	default:
		return fmt.Errorf("unknown command %q (want serve, migrate, fixtures or healthcheck)", command)
	}
}

func serve(ctx context.Context, cfg Config, log *slog.Logger) error {
	if err := cfg.validateServe(); err != nil {
		return err
	}
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL, postgresPoolConfig())
	if err != nil {
		return err
	}
	defer pool.Close()

	redisClient := ratelimit.NewRedis(ratelimit.RedisConfig{
		Addr:         cfg.RedisAddr,
		Log:          log,
		DialTimeout:  redisDialTimeout,
		ReadTimeout:  redisReadTimeout,
		WriteTimeout: redisWriteTimeout,
		PoolSize:     redisPoolSize,
	})
	defer func() { _ = redisClient.Close() }()

	hydraHTTP := &http.Client{Timeout: hydraHTTPTimeout}
	jwks := hydra.NewJWKS(hydra.JWKSConfig{
		URL:                cfg.HydraJWKSURL,
		TTL:                jwksTTL,
		MinRefreshInterval: jwksMinRefresh,
		Retry: retry.Config{
			MaxAttempts: jwksRetryAttempts,
			BaseDelay:   jwksRetryBaseDelay,
			MaxDelay:    jwksRetryMaxDelay,
		},
		FetchTimeout: jwksFetchTimeout,
	}, hydraHTTP, log)
	verifier := hydra.NewVerifier(hydra.VerifierConfig{
		Issuer:   cfg.HydraIssuer,
		ClientID: cfg.OAuthClientID,
		Leeway:   tokenLeeway,
	}, jwks)
	// Hydra creates its signing keys on the first request, which can take a
	// moment. Asking now means the first real request and /readyz find them loaded.
	go func() {
		if err := jwks.Ready(ctx); err != nil {
			log.Warn("could not load Hydra signing keys at startup, will retry on demand", slog.Any("error", err))
		}
	}()
	oauth := hydra.NewOAuthClient(hydra.OAuthConfig{
		ClientID:     cfg.OAuthClientID,
		ClientSecret: cfg.OAuthClientSecret,
		RedirectURL:  cfg.OAuthRedirectURL,
		AuthURL:      cfg.HydraAuthURL,
		TokenURL:     cfg.HydraTokenURL,
		Scopes:       splitScopes(oauthScopes),
	}, hydraHTTP)

	store := repository.NewStore(pool)
	access := usecase.NewAccessControl(store, ratelimit.NewLimiter(redisClient, breaker.New(breakerThreshold, breakerCooldown)), log)

	var draining atomic.Bool
	handler := transport.NewHandler(
		transport.Config{
			RequestTimeout: requestTimeout,
			MaxBodyBytes:   maxBodyBytes,
			SecureCookies:  cfg.SecureCookies,
		},
		transport.Deps{
			Auth:      usecase.NewAuth(verifier, usecase.NewUsers(store)),
			Projects:  usecase.NewProjects(store, access),
			Sandboxes: usecase.NewSandboxes(store, access),
			OAuth:     oauth,
			Readiness: map[string]transport.ReadinessCheck{
				"postgres": pool.Ping,
				"redis":    func(ctx context.Context) error { return ratelimit.Ping(ctx, redisClient) },
				"hydra":    jwks.Ready,
			},
			Draining: &draining,
			Log:      log,
		},
	)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
	}
	log.Info("starting",
		slog.String("addr", cfg.HTTPAddr), slog.String("issuer", cfg.HydraIssuer),
		slog.String("redis", cfg.RedisAddr), slog.String("oauth_client", cfg.OAuthClientID))
	return listenAndServe(ctx, server, &draining, log)
}

// listenAndServe runs the server until ctx is cancelled, then drains
// in-flight requests before returning.
func listenAndServe(ctx context.Context, server *http.Server, draining *atomic.Bool, log *slog.Logger) error {
	failed := make(chan error, 1)
	go func() {
		log.Info("listening", slog.String("addr", server.Addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			failed <- err
		}
	}()

	select {
	case err := <-failed:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	draining.Store(true)
	log.Info("shutting down", slog.String("grace", shutdownTimeout.String()))
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		// Slow clients kept connections open. Cut them so the deferred pool
		// and Redis closes in serve still run before the process is killed.
		_ = server.Close()
		log.Warn("graceful shutdown timed out, closed remaining connections", slog.Any("error", err))
		return nil
	}
	log.Info("shutdown complete")
	return nil
}

// maintenancePoolConfig is for migrate and fixtures: the same pool without the
// per statement timeout that protects request handling.
func maintenancePoolConfig() postgres.PoolConfig {
	cfg := postgresPoolConfig()
	cfg.StatementTimeout = maintenanceStatementTimeout
	return cfg
}

func postgresPoolConfig() postgres.PoolConfig {
	return postgres.PoolConfig{
		MaxConns:         dbMaxConns,
		MinConns:         dbMinConns,
		MaxConnLifetime:  dbMaxConnLifetime,
		MaxConnIdleTime:  dbMaxConnIdleTime,
		HealthCheckEvery: dbHealthCheckEvery,
		StatementTimeout: dbStatementTimeout,
		LockTimeout:      dbLockTimeout,
	}
}

func migrate(ctx context.Context, cfg Config) error {
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL, maintenancePoolConfig())
	if err != nil {
		return err
	}
	defer pool.Close()

	results, err := postgres.Migrate(ctx, pool)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Println("migrations: database is up to date")
	}
	for _, r := range results {
		fmt.Printf("migrations: applied %s in %s\n", r.Source.Path, r.Duration.Round(time.Millisecond))
	}
	return nil
}

func fixtures(ctx context.Context, cfg Config) error {
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL, maintenancePoolConfig())
	if err != nil {
		return err
	}
	defer pool.Close()

	n, err := postgres.ApplyFixtures(ctx, pool, postgres.FixtureData{Issuer: cfg.HydraIssuer})
	if err != nil {
		return err
	}
	fmt.Printf("fixtures: applied %d file(s)\n", n)
	return nil
}

// healthcheck probes the running server from inside its own container, which
// lets the image stay minimal: no curl or wget needed. It checks liveness
// only. Dependencies are reported by /readyz, because a Redis outage must not
// mark a server unhealthy when the limiter is designed to fail open.
func healthcheck(ctx context.Context, cfg Config) error {
	ctx, cancel := context.WithTimeout(ctx, healthcheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1"+localPort(cfg.HTTPAddr)+healthcheckPath, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("probe: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("probe: status %d", resp.StatusCode)
	}
	return nil
}

// localPort returns ":port" for addresses like ":8080" or "0.0.0.0:8080".
func localPort(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[i:]
		}
	}
	return defaultPort
}
