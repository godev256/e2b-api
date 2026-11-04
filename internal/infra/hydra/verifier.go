package hydra

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/buildoshi/e2b-api/internal/models"
)

// VerifierConfig describes which tokens are acceptable.
type VerifierConfig struct {
	Issuer string
	// ClientID is the OAuth client our tokens are issued to. Hydra signs ID
	// tokens and tokens of every other client with the same keys, so the
	// signature alone does not prove a token was meant for this API.
	ClientID string
	// Leeway tolerates small clock differences between us and the provider.
	Leeway time.Duration
}

// Verifier validates access tokens issued by Hydra.
type Verifier struct {
	jwks     *JWKS
	parser   *jwt.Parser
	clientID string
}

func NewVerifier(cfg VerifierConfig, jwks *JWKS) *Verifier {
	return &Verifier{
		jwks:     jwks,
		clientID: cfg.ClientID,
		parser: jwt.NewParser(
			// Pinning the algorithm blocks "alg: none" and RS/HS confusion attacks.
			jwt.WithValidMethods([]string{signingAlgorithm}),
			jwt.WithIssuer(cfg.Issuer),
			jwt.WithLeeway(cfg.Leeway),
			jwt.WithExpirationRequired(),
		),
	}
}

type accessClaims struct {
	jwt.RegisteredClaims
	// ClientID is present in Hydra access tokens and absent from ID tokens.
	ClientID string `json:"client_id"`
	Email    string `json:"email"`
}

// Verify checks signature, issuer, validity window and the issuing client. It
// deliberately does not require `aud`: the provider issues access tokens with
// an empty audience, so `client_id` is what ties a token to this API.
func (v *Verifier) Verify(ctx context.Context, rawToken string) (models.Claims, error) {
	var claims accessClaims
	_, err := v.parser.ParseWithClaims(rawToken, &claims, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("token has no key id")
		}
		return v.jwks.Key(ctx, kid)
	})
	if err != nil {
		if errors.Is(err, ErrKeysUnavailable) {
			return models.Claims{}, err // our dependency failed, not the caller's token
		}
		return models.Claims{}, fmt.Errorf("%w: %w", models.ErrUnauthenticated, err)
	}
	if claims.Subject == "" {
		return models.Claims{}, fmt.Errorf("%w: token has no subject", models.ErrUnauthenticated)
	}
	if claims.ClientID != v.clientID {
		return models.Claims{}, fmt.Errorf("%w: token was not issued to this API's client", models.ErrUnauthenticated)
	}
	return models.Claims{Issuer: claims.Issuer, Subject: claims.Subject, Email: claims.Email}, nil
}
