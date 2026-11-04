package usecase

import (
	"context"
	"net/mail"
	"strings"

	"github.com/buildoshi/e2b-api/internal/models"
	"github.com/buildoshi/e2b-api/pkg/idgen"
	"github.com/buildoshi/e2b-api/pkg/ptr"
	"github.com/buildoshi/e2b-api/pkg/ttlcache"
)

type identity struct{ issuer, subject string }

// Users maps a verified token identity to a local user row.
type Users struct {
	store Store
	cache *ttlcache.Cache[identity, models.User]
}

func NewUsers(store Store) *Users {
	return &Users{
		store: store,
		cache: ttlcache.New[identity, models.User](userCacheTTL, userCacheMaxEntries, cacheLoadTimeout),
	}
}

// Resolve returns the user for the claims, creating it on first sight. Warm
// identities are served from memory, so authentication costs no query.
func (u *Users) Resolve(ctx context.Context, claims models.Claims) (models.User, error) {
	key := identity{issuer: claims.Issuer, subject: claims.Subject}
	return u.cache.GetOrLoad(ctx, key, func(ctx context.Context) (models.User, error) {
		return u.store.UpsertUser(ctx, models.User{
			ID:      idgen.New(),
			Issuer:  claims.Issuer,
			Subject: claims.Subject,
			Email:   emailOf(claims),
		})
	})
}

// emailOf returns the lowercase email for the claims, or nil when unknown.
//
// The demo Hydra consent app puts the login email into `sub` and issues no
// `email` claim, so a subject that parses as an address is accepted as one.
func emailOf(claims models.Claims) *string {
	for _, candidate := range []string{claims.Email, claims.Subject} {
		if email, ok := normalizeEmail(candidate); ok {
			return ptr.To(email)
		}
	}
	return nil
}

func normalizeEmail(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > MaxEmailLength {
		return "", false
	}
	addr, err := mail.ParseAddress(raw)
	if err != nil || addr.Address != raw {
		return "", false
	}
	return strings.ToLower(raw), true
}
