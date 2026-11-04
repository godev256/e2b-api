package hydra_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/buildoshi/e2b-api/internal/infra/hydra"
	"github.com/buildoshi/e2b-api/internal/models"
	"github.com/buildoshi/e2b-api/pkg/retry"
)

const (
	testIssuer    = "http://issuer.test"
	testClientID  = "api-client"
	testKeyID     = "key-1"
	rsaBits       = 2048
	tokenLifetime = time.Hour
	concurrency   = 40
)

var fastRetry = retry.Config{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond}

// provider is a fake OAuth provider that publishes one signing key.
type provider struct {
	key     *rsa.PrivateKey
	server  *httptest.Server
	fetches atomic.Int32
	status  atomic.Int32 // when non-zero, jwks answers with this status
}

func newProvider(t *testing.T) *provider {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, rsaBits)
	require.NoError(t, err)

	p := &provider{key: key}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p.fetches.Add(1)
		if code := int(p.status.Load()); code != 0 {
			w.WriteHeader(code)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "use": "sig", "kid": testKeyID,
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(p.server.Close)
	return p
}

func (p *provider) jwks(minRefresh time.Duration) *hydra.JWKS {
	return hydra.NewJWKS(hydra.JWKSConfig{
		URL: p.server.URL, TTL: time.Hour, MinRefreshInterval: minRefresh, Retry: fastRetry, FetchTimeout: time.Second,
	}, p.server.Client(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func (p *provider) verifier(jwks *hydra.JWKS) *hydra.Verifier {
	return hydra.NewVerifier(hydra.VerifierConfig{Issuer: testIssuer, ClientID: testClientID, Leeway: time.Second}, jwks)
}

func (p *provider) sign(t *testing.T, method jwt.SigningMethod, key any, kid string, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	require.NoError(t, err)
	return signed
}

func validClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"iss":       testIssuer,
		"sub":       "foo@bar.com",
		"client_id": testClientID,
		"exp":       time.Now().Add(tokenLifetime).Unix(),
		"nbf":       time.Now().Add(-time.Minute).Unix(),
		"aud":       []string{}, // Hydra issues an empty audience for this client
	}
}

func TestVerifyAcceptsValidToken(t *testing.T) {
	p := newProvider(t)
	v := p.verifier(p.jwks(0))

	claims, err := v.Verify(context.Background(), p.sign(t, jwt.SigningMethodRS256, p.key, testKeyID, validClaims()))

	require.NoError(t, err)
	require.Equal(t, models.Claims{Issuer: testIssuer, Subject: "foo@bar.com"}, claims)
}

func TestVerifyRejectsBadTokens(t *testing.T) {
	p := newProvider(t)
	v := p.verifier(p.jwks(0))
	otherKey, err := rsa.GenerateKey(rand.Reader, rsaBits)
	require.NoError(t, err)

	with := func(mutate func(jwt.MapClaims)) jwt.MapClaims {
		c := validClaims()
		mutate(c)
		return c
	}

	tests := map[string]string{
		"expired":                    p.sign(t, jwt.SigningMethodRS256, p.key, testKeyID, with(func(c jwt.MapClaims) { c["exp"] = time.Now().Add(-time.Hour).Unix() })),
		"wrong issuer":               p.sign(t, jwt.SigningMethodRS256, p.key, testKeyID, with(func(c jwt.MapClaims) { c["iss"] = "http://evil.test" })),
		"no expiry":                  p.sign(t, jwt.SigningMethodRS256, p.key, testKeyID, with(func(c jwt.MapClaims) { delete(c, "exp") })),
		"id token without client_id": p.sign(t, jwt.SigningMethodRS256, p.key, testKeyID, with(func(c jwt.MapClaims) { delete(c, "client_id") })),
		"other oauth client":         p.sign(t, jwt.SigningMethodRS256, p.key, testKeyID, with(func(c jwt.MapClaims) { c["client_id"] = "someone-else" })),
		"no subject":                 p.sign(t, jwt.SigningMethodRS256, p.key, testKeyID, with(func(c jwt.MapClaims) { delete(c, "sub") })),
		"signed by stranger":         p.sign(t, jwt.SigningMethodRS256, otherKey, testKeyID, validClaims()),
		"unknown key id":             p.sign(t, jwt.SigningMethodRS256, p.key, "other-kid", validClaims()),
		"alg none":                   p.sign(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, testKeyID, validClaims()),
		"hmac with public n":         p.sign(t, jwt.SigningMethodHS256, p.key.N.Bytes(), testKeyID, validClaims()),
		"not a jwt":                  "definitely.not.jwt",
	}
	for name, token := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := v.Verify(context.Background(), token)
			require.ErrorIs(t, err, models.ErrUnauthenticated)
		})
	}
}

func TestVerifyReportsUnavailableKeysAsInfrastructureError(t *testing.T) {
	p := newProvider(t)
	p.status.Store(http.StatusServiceUnavailable)
	v := p.verifier(p.jwks(0))

	_, err := v.Verify(context.Background(), p.sign(t, jwt.SigningMethodRS256, p.key, testKeyID, validClaims()))

	require.ErrorIs(t, err, hydra.ErrKeysUnavailable)
	require.ErrorIs(t, err, models.ErrAuthUnavailable)
	require.NotErrorIs(t, err, models.ErrUnauthenticated, "a provider outage must not look like a bad token")
}

func TestJWKSCoalescesConcurrentRefreshes(t *testing.T) {
	p := newProvider(t)
	jwks := p.jwks(0)

	var wg sync.WaitGroup
	failures := make(chan error, concurrency)
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := jwks.Key(context.Background(), testKeyID); err != nil {
				failures <- err
			}
		}()
	}
	wg.Wait()
	close(failures)
	require.Empty(t, failures)

	// Callers that arrive after the first fetch finished are served from cache,
	// so there is at most a handful of fetches, never one per caller.
	require.Less(t, int(p.fetches.Load()), concurrency/2)
}

func TestJWKSDoesNotRefetchForUnknownKeyTooOften(t *testing.T) {
	p := newProvider(t)
	jwks := p.jwks(time.Hour)

	_, err := jwks.Key(context.Background(), testKeyID)
	require.NoError(t, err)
	fetchesAfterWarmup := p.fetches.Load()

	for range 10 {
		_, err := jwks.Key(context.Background(), "attacker-chosen-kid")
		require.ErrorIs(t, err, hydra.ErrUnknownKey)
	}

	require.Equal(t, fetchesAfterWarmup, p.fetches.Load(), "random key ids must not trigger provider calls")
}

func TestJWKSRetriesTransientProviderErrors(t *testing.T) {
	p := newProvider(t)
	p.status.Store(http.StatusBadGateway)
	jwks := p.jwks(0)

	_, err := jwks.Key(context.Background(), testKeyID)

	require.ErrorIs(t, err, hydra.ErrKeysUnavailable)
	require.EqualValues(t, fastRetry.MaxAttempts, p.fetches.Load())
}

func TestUnknownKeyDuringAnOutageIsReportedAsUnavailableNotUnauthenticated(t *testing.T) {
	p := newProvider(t)
	p.status.Store(http.StatusServiceUnavailable)
	jwks := p.jwks(time.Hour) // the failed attempt below throttles further refreshes

	_, err := jwks.Key(context.Background(), testKeyID)
	require.ErrorIs(t, err, hydra.ErrKeysUnavailable)

	_, err = jwks.Key(context.Background(), "key-from-after-the-restart")
	require.ErrorIs(t, err, hydra.ErrKeysUnavailable, "a good token must not be called invalid just because the provider is down")
}
