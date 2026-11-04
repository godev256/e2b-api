package usecase

import (
	"context"

	"github.com/buildoshi/e2b-api/internal/models"
)

// Auth turns a bearer token into a local user.
type Auth struct {
	verifier TokenVerifier
	users    *Users
}

func NewAuth(verifier TokenVerifier, users *Users) *Auth {
	return &Auth{verifier: verifier, users: users}
}

// Authenticate verifies the token and resolves its owner, creating the user
// on first sight. Sign up and sign in are the same step.
func (a *Auth) Authenticate(ctx context.Context, rawToken string) (models.User, error) {
	claims, err := a.verifier.Verify(ctx, rawToken)
	if err != nil {
		return models.User{}, err
	}
	return a.users.Resolve(ctx, claims)
}
