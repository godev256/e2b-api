package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/buildoshi/e2b-api/internal/models"
	"github.com/buildoshi/e2b-api/pkg/breaker"
	"github.com/buildoshi/e2b-api/pkg/ttlcache"
)

// Grant is the proof that a user may act inside a project, together with the
// limits of the project's plan.
type Grant struct {
	Access models.ProjectAccess
	Plan   models.Plan
}

// AccessControl answers two questions for every request: may this user touch
// this project, and is the caller within the plan's rate limit.
type AccessControl struct {
	store   Store
	limiter RateLimiter
	plans   *ttlcache.Cache[string, models.Plan]
	log     *slog.Logger
}

func NewAccessControl(store Store, limiter RateLimiter, log *slog.Logger) *AccessControl {
	return &AccessControl{
		store:   store,
		limiter: limiter,
		plans:   ttlcache.New[string, models.Plan](planCacheTTL, planCacheMaxEntries, cacheLoadTimeout),
		log:     log,
	}
}

// AuthorizeProject returns models.ErrNotFound unless the user is a member,
// then charges one request against the project's rate limit.
func (a *AccessControl) AuthorizeProject(ctx context.Context, userID, projectID uuid.UUID) (Grant, error) {
	access, err := a.store.GetProjectAccess(ctx, projectID, userID)
	if errors.Is(err, models.ErrNotFound) {
		return Grant{}, a.Miss(ctx, userID)
	}
	if err != nil {
		return Grant{}, err
	}
	plan, err := a.plan(ctx, access.PlanCode)
	if err != nil {
		return Grant{}, fmt.Errorf("load plan %q: %w", access.PlanCode, err)
	}
	if err := a.charge(ctx, models.RateKey{Tenant: fmt.Sprintf(projectTenantFormat, projectID)}, planLimit(plan)); err != nil {
		return Grant{}, err
	}
	return Grant{Access: access, Plan: plan}, nil
}

// Miss is the answer for a resource that does not exist or is not the
// caller's: models.ErrNotFound, or a rate limit error if the caller keeps
// asking. Without it, random ids would be a free way to load the database.
func (a *AccessControl) Miss(ctx context.Context, userID uuid.UUID) error {
	if err := a.charge(ctx, models.RateKey{Tenant: fmt.Sprintf(userTenantFormat, userID), Bucket: missBucket}, unknownResourceLimit); err != nil {
		return err
	}
	return models.ErrNotFound
}

// LimitUser charges one request against a per-user bucket. It guards the
// endpoints that are not tied to a project yet, using the default plan.
func (a *AccessControl) LimitUser(ctx context.Context, userID uuid.UUID) error {
	plan, err := a.plan(ctx, NewProjectPlan)
	if err != nil {
		return fmt.Errorf("load plan %q: %w", NewProjectPlan, err)
	}
	return a.charge(ctx, models.RateKey{Tenant: fmt.Sprintf(userTenantFormat, userID)}, planLimit(plan))
}

func (a *AccessControl) plan(ctx context.Context, code string) (models.Plan, error) {
	return a.plans.GetOrLoad(ctx, code, func(ctx context.Context) (models.Plan, error) {
		return a.store.GetPlan(ctx, code)
	})
}

func planLimit(plan models.Plan) models.RateLimit {
	return models.RateLimit{PerSecond: plan.MaxRequestsPerSecond, Burst: plan.Burst}
}

// charge fails open: if Redis is unavailable the request goes through, because
// refusing all traffic over a cache outage is worse than briefly not limiting.
// The quotas live in Postgres and stay enforced either way.
func (a *AccessControl) charge(ctx context.Context, key models.RateKey, limit models.RateLimit) error {
	decision, err := a.limiter.Allow(ctx, key, limit)
	if err != nil {
		level := slog.LevelWarn
		if errors.Is(err, breaker.ErrOpen) {
			level = slog.LevelDebug // the breaker already announced the outage
		}
		a.log.Log(ctx, level, "rate limiter unavailable, allowing request", slog.String("tenant", key.Tenant), slog.String("bucket", key.Bucket), slog.Any("error", err))
		return nil
	}
	if !decision.Allowed {
		return &models.RateLimitError{Decision: decision}
	}
	return nil
}
