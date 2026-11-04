package app

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config is read from the environment. Non secret defaults match a service
// started on the host next to `docker compose up`, and docker-compose.yml
// overrides the hosts. Secrets and the database URL have no default, so a
// forgotten variable fails at startup instead of running with a demo value.
type Config struct {
	HTTPAddr    string
	DatabaseURL string
	RedisAddr   string

	// HydraIssuer must equal the `iss` claim of the tokens (what Hydra advertises).
	HydraIssuer string
	// HydraAuthURL is opened by the browser. HydraTokenURL and HydraJWKSURL are
	// called by this service, so inside docker they use the compose hostname.
	HydraAuthURL  string
	HydraTokenURL string
	HydraJWKSURL  string

	OAuthClientID     string
	OAuthClientSecret string
	OAuthRedirectURL  string

	LogFormat     string
	LogLevel      string
	SecureCookies bool
}

// LoadConfig reads and validates the configuration.
func LoadConfig() (Config, error) {
	cfg := Config{
		HTTPAddr:          env("HTTP_ADDR", ":8080"),
		DatabaseURL:       os.Getenv("DATABASE_URL"),
		RedisAddr:         env("REDIS_ADDR", "localhost:6379"),
		HydraIssuer:       env("HYDRA_ISSUER", "http://localhost:4444"),
		HydraAuthURL:      env("HYDRA_AUTH_URL", "http://localhost:4444/oauth2/auth"),
		HydraTokenURL:     env("HYDRA_TOKEN_URL", "http://localhost:4444/oauth2/token"),
		HydraJWKSURL:      env("HYDRA_JWKS_URL", "http://localhost:4444/.well-known/jwks.json"),
		OAuthClientID:     env("OAUTH_CLIENT_ID", "e2b-assignment"),
		OAuthClientSecret: os.Getenv("OAUTH_CLIENT_SECRET"),
		OAuthRedirectURL:  env("OAUTH_REDIRECT_URL", "http://localhost:8080/auth/callback"),
		LogFormat:         env("LOG_FORMAT", "json"),
		LogLevel:          env("LOG_LEVEL", "info"),
	}
	secure, err := strconv.ParseBool(env("SECURE_COOKIES", "false"))
	if err != nil {
		return Config{}, fmt.Errorf("SECURE_COOKIES: %w", err)
	}
	cfg.SecureCookies = secure

	return cfg, cfg.validateCommon()
}

// validateCommon checks what every command needs.
func (c Config) validateCommon() error {
	var errs []error
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	for name, addr := range map[string]string{"HTTP_ADDR": c.HTTPAddr, "REDIS_ADDR": c.RedisAddr} {
		if _, _, err := net.SplitHostPort(addr); err != nil {
			errs = append(errs, fmt.Errorf("%s must look like host:port, got %q", name, addr))
		}
	}
	if c.LogFormat != "json" && c.LogFormat != "text" {
		errs = append(errs, fmt.Errorf("LOG_FORMAT must be json or text, got %q", c.LogFormat))
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(c.LogLevel)); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error, got %q", c.LogLevel))
	}
	return errors.Join(errs...)
}

// validateServe checks what only the HTTP server needs. The migrate and
// fixtures commands run without any OAuth settings.
func (c Config) validateServe() error {
	var errs []error
	for name, raw := range map[string]string{
		"HYDRA_ISSUER":       c.HydraIssuer,
		"HYDRA_AUTH_URL":     c.HydraAuthURL,
		"HYDRA_TOKEN_URL":    c.HydraTokenURL,
		"HYDRA_JWKS_URL":     c.HydraJWKSURL,
		"OAUTH_REDIRECT_URL": c.OAuthRedirectURL,
	} {
		if u, err := url.Parse(raw); err != nil || u.Scheme == "" || u.Host == "" {
			errs = append(errs, fmt.Errorf("%s must be an absolute URL, got %q", name, raw))
		}
	}
	if c.OAuthClientID == "" || c.OAuthClientSecret == "" {
		errs = append(errs, errors.New("OAUTH_CLIENT_ID and OAUTH_CLIENT_SECRET are required"))
	}
	return errors.Join(errs...)
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func splitScopes(s string) []string { return strings.Fields(s) }
