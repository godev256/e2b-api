// Package hydra talks to the OAuth2 provider: key discovery, token
// verification and the authorization code flow.
package hydra

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/buildoshi/e2b-api/internal/models"
	"github.com/buildoshi/e2b-api/pkg/retry"
)

// ErrKeysUnavailable means the signing keys could not be fetched. It is an
// infrastructure failure, not a verdict on the token, so it matches
// models.ErrAuthUnavailable and the transport layer answers 503.
var ErrKeysUnavailable = fmt.Errorf("signing keys unavailable: %w", models.ErrAuthUnavailable)

// ErrUnknownKey means the token names a key the provider does not publish.
var ErrUnknownKey = errors.New("unknown signing key")

// JWKSConfig controls key caching and refetching.
type JWKSConfig struct {
	URL string
	// TTL is how long keys are trusted before the next request refreshes them.
	TTL time.Duration
	// MinRefreshInterval caps how often an unknown key id may trigger a fetch.
	// Without it, a caller sending random key ids could make us hammer the provider.
	MinRefreshInterval time.Duration
	Retry              retry.Config
	// FetchTimeout bounds one refresh including its retries.
	FetchTimeout time.Duration
}

// JWKS caches the provider's public signing keys.
//
// Hydra keeps keys in memory, so a restart rotates them and every token that
// arrives afterwards names a key we have never seen. All of those requests
// wait on one shared fetch (singleflight) instead of each calling the provider.
type JWKS struct {
	cfg    JWKSConfig
	client *http.Client
	log    *slog.Logger

	group singleflight.Group

	mu          sync.RWMutex
	keys        map[string]*rsa.PublicKey
	fetchedAt   time.Time
	attemptedAt time.Time
	lastFailed  bool
}

func NewJWKS(cfg JWKSConfig, client *http.Client, log *slog.Logger) *JWKS {
	return &JWKS{cfg: cfg, client: client, log: log, keys: map[string]*rsa.PublicKey{}}
}

// Key returns the public key for kid, fetching keys when needed.
func (j *JWKS) Key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	key, known, stale := j.lookup(kid)
	if known && !stale {
		return key, nil
	}
	if !j.mayRefresh() {
		if known {
			return key, nil
		}
		if j.lastRefreshFailed() {
			// The provider is down, so this may be a perfectly good token.
			return nil, ErrKeysUnavailable
		}
		return nil, ErrUnknownKey
	}

	if err := j.refresh(ctx); err != nil {
		if known {
			// A stale key still verifies signatures, so prefer it to an outage.
			j.log.WarnContext(ctx, "jwks refresh failed, using cached key", slog.Any("error", err))
			return key, nil
		}
		return nil, err
	}
	if key, known, _ = j.lookup(kid); known {
		return key, nil
	}
	return nil, ErrUnknownKey
}

// Ready reports whether keys have been loaded at least once.
func (j *JWKS) Ready(ctx context.Context) error {
	j.mu.RLock()
	loaded := len(j.keys) > 0
	j.mu.RUnlock()
	if loaded {
		return nil
	}
	return j.refresh(ctx)
}

func (j *JWKS) lookup(kid string) (key *rsa.PublicKey, known, stale bool) {
	j.mu.RLock()
	defer j.mu.RUnlock()
	key, known = j.keys[kid]
	return key, known, time.Since(j.fetchedAt) > j.cfg.TTL
}

func (j *JWKS) lastRefreshFailed() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.lastFailed
}

func (j *JWKS) mayRefresh() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return time.Since(j.attemptedAt) >= j.cfg.MinRefreshInterval
}

// refresh fetches the key set once for all concurrent callers. The fetch is
// detached from any single caller (one cancelled request must not fail the
// others) and bounded by its own timeout, while each caller stops waiting when
// its own context is done.
func (j *JWKS) refresh(ctx context.Context) error {
	flight := j.group.DoChan("refresh", func() (any, error) {
		j.mu.Lock()
		j.attemptedAt = time.Now()
		j.mu.Unlock()

		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), j.cfg.FetchTimeout)
		defer cancel()
		keys, err := retry.Value(fetchCtx, j.cfg.Retry, isTransientFetch, j.fetch)

		j.mu.Lock()
		defer j.mu.Unlock()
		if err != nil {
			j.lastFailed = true
			return nil, fmt.Errorf("%w: %w", ErrKeysUnavailable, err)
		}
		j.keys = keys
		j.fetchedAt = time.Now()
		j.lastFailed = false
		return nil, nil
	})

	select {
	case <-ctx.Done():
		return fmt.Errorf("%w: %w", ErrKeysUnavailable, ctx.Err())
	case res := <-flight:
		return res.Err
	}
}

type jwkSet struct {
	Keys []struct {
		Kty string `json:"kty"`
		Use string `json:"use"`
		Kid string `json:"kid"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

// statusError carries an unexpected HTTP status so the retry policy can tell
// a provider hiccup (5xx) from a configuration problem (4xx).
type statusError struct{ code int }

func (e *statusError) Error() string { return fmt.Sprintf("unexpected status %d", e.code) }

func isTransientFetch(err error) bool {
	var se *statusError
	if errors.As(err, &se) {
		return se.code >= http.StatusInternalServerError || se.code == http.StatusTooManyRequests
	}
	return true // network errors and timeouts
}

func (j *JWKS) fetch(ctx context.Context) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.cfg.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build jwks request: %w", err)
	}
	resp, err := j.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &statusError{code: resp.StatusCode}
	}

	var set jwkSet
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJWKSBytes)).Decode(&set); err != nil {
		return nil, fmt.Errorf("decode jwks: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(set.Keys))
	for _, k := range set.Keys {
		if k.Kty != "RSA" || (k.Use != "" && k.Use != "sig") {
			continue
		}
		pub, err := parseRSA(k.N, k.E)
		if err != nil {
			return nil, fmt.Errorf("parse key %q: %w", k.Kid, err)
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return nil, errors.New("jwks contains no usable RSA signing keys")
	}
	return keys, nil
}

func parseRSA(n, e string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(n)
	if err != nil {
		return nil, fmt.Errorf("decode modulus: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(e)
	if err != nil {
		return nil, fmt.Errorf("decode exponent: %w", err)
	}
	exponent := int(new(big.Int).SetBytes(eBytes).Int64())
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: exponent}, nil
}
