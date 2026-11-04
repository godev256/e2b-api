package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// setBaseEnv sets the minimum a valid configuration needs.
func setBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db")
	t.Setenv("OAUTH_CLIENT_SECRET", "secret")
}

func TestLoadConfigAcceptsTheMinimumEnvironment(t *testing.T) {
	setBaseEnv(t)

	cfg, err := LoadConfig()

	require.NoError(t, err)
	require.Equal(t, ":8080", cfg.HTTPAddr)
	require.NoError(t, cfg.validateServe())
}

func TestLoadConfigRequiresTheDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	_, err := LoadConfig()

	require.ErrorContains(t, err, "DATABASE_URL is required")
}

func TestServeRequiresAnOAuthSecretButMigrateDoesNot(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/db")
	t.Setenv("OAUTH_CLIENT_SECRET", "")

	cfg, err := LoadConfig()

	require.NoError(t, err, "migrate and fixtures need only the database")
	require.ErrorContains(t, cfg.validateServe(), "OAUTH_CLIENT_SECRET")
}

func TestLoadConfigRejectsBadValuesWithAClearMessage(t *testing.T) {
	tests := map[string]struct{ key, value, want string }{
		"address without port": {"REDIS_ADDR", "localhost", "REDIS_ADDR"},
		"unknown log format":   {"LOG_FORMAT", "xml", "LOG_FORMAT"},
		"unknown log level":    {"LOG_LEVEL", "loud", "LOG_LEVEL"},
		"bad boolean":          {"SECURE_COOKIES", "maybe", "SECURE_COOKIES"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			setBaseEnv(t)
			t.Setenv(tc.key, tc.value)

			_, err := LoadConfig()

			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestServeRejectsRelativeHydraURLs(t *testing.T) {
	setBaseEnv(t)
	t.Setenv("HYDRA_JWKS_URL", "/just/a/path")
	cfg, err := LoadConfig()
	require.NoError(t, err)

	require.ErrorContains(t, cfg.validateServe(), "HYDRA_JWKS_URL")
}
