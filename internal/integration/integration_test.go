//go:build integration

// Package integration tests the layers together against real PostgreSQL and
// Redis. Start the stack first (make start), then run: make test-integration.
package integration_test

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/buildoshi/e2b-api/internal/infra/postgres"
	"github.com/buildoshi/e2b-api/internal/infra/ratelimit"
	"github.com/buildoshi/e2b-api/internal/models"
	"github.com/buildoshi/e2b-api/internal/repository"
	"github.com/buildoshi/e2b-api/internal/usecase"
	"github.com/buildoshi/e2b-api/pkg/breaker"
)

const (
	defaultDatabaseURL = "postgres://e2b:e2b@localhost:5432/e2b?sslmode=disable"
	defaultRedisAddr   = "localhost:6379"
	raceCallers        = 30
	testIssuer         = "http://integration.test"
)

var ctx = context.Background()

type env struct {
	store     *repository.Store
	users     *usecase.Users
	projects  *usecase.Projects
	sandboxes *usecase.Sandboxes
}

// setup creates a private database for the test, so runs never touch the dev
// data and never leave rows behind. The database is dropped when the test ends.
func setup(t *testing.T, limiter usecase.RateLimiter) env {
	t.Helper()
	adminURL := getenv("DATABASE_URL", defaultDatabaseURL)
	name := "e2b_it_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]

	admin, err := pgx.Connect(ctx, adminURL)
	require.NoError(t, err, "is the stack running? try: make start")
	_, err = admin.Exec(ctx, "CREATE DATABASE "+name)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = admin.Close(ctx)
	})

	u, err := url.Parse(adminURL)
	require.NoError(t, err)
	u.Path = "/" + name

	pool, err := postgres.NewPool(ctx, u.String(), postgres.PoolConfig{
		MaxConns: 40, MaxConnLifetime: time.Hour, MaxConnIdleTime: time.Minute,
		HealthCheckEvery: time.Minute, StatementTimeout: 5 * time.Second, LockTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = postgres.Migrate(ctx, pool)
	require.NoError(t, err)

	store := repository.NewStore(pool)
	access := usecase.NewAccessControl(store, limiter, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return env{
		store:     store,
		users:     usecase.NewUsers(store),
		projects:  usecase.NewProjects(store, access),
		sandboxes: usecase.NewSandboxes(store, access),
	}
}

func (e env) newUser(t *testing.T) models.User {
	t.Helper()
	subject := uuid.NewString() + "@example.com"
	user, err := e.users.Resolve(ctx, models.Claims{Issuer: testIssuer, Subject: subject})
	require.NoError(t, err)
	return user
}

type allowAll struct{}

func (allowAll) Allow(context.Context, models.RateKey, models.RateLimit) (models.RateDecision, error) {
	return models.RateDecision{Allowed: true}, nil
}

func TestConcurrentSandboxCreationNeverExceedsQuota(t *testing.T) {
	e := setup(t, allowAll{})
	owner := e.newUser(t)
	project, err := e.projects.Create(ctx, owner.ID, "race")
	require.NoError(t, err)
	plan, err := e.store.GetPlan(ctx, project.PlanCode)
	require.NoError(t, err)

	var wg sync.WaitGroup
	results := make(chan error, raceCallers)
	for range raceCallers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.sandboxes.Create(ctx, owner.ID, project.ID, "")
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	var created, refused int
	for err := range results {
		var quota *models.SandboxQuotaError
		switch {
		case err == nil:
			created++
		case errorsAs(err, &quota):
			refused++
		default:
			require.NoError(t, err)
		}
	}
	running, err := e.store.CountRunningSandboxes(ctx, project.ID)
	require.NoError(t, err)

	require.Equal(t, plan.MaxRunningSandboxes, created, "exactly the plan limit succeeds")
	require.Equal(t, raceCallers-created, refused)
	require.Equal(t, plan.MaxRunningSandboxes, running)
}

func TestSharingGivesTheOtherUserAccess(t *testing.T) {
	e := setup(t, allowAll{})
	alice, bob, mallory := e.newUser(t), e.newUser(t), e.newUser(t)
	project, err := e.projects.Create(ctx, alice.ID, "shared")
	require.NoError(t, err)
	sandbox, err := e.sandboxes.Create(ctx, alice.ID, project.ID, "")
	require.NoError(t, err)

	_, err = e.sandboxes.List(ctx, bob.ID, project.ID, models.Page{})
	require.ErrorIs(t, err, models.ErrNotFound, "before sharing Bob sees nothing")

	_, created, err := e.projects.AddMember(ctx, alice.ID, project.ID, *bob.Email)
	require.NoError(t, err)
	require.True(t, created)

	listed, err := e.sandboxes.List(ctx, bob.ID, project.ID, models.Page{})
	require.NoError(t, err)
	require.Len(t, listed.Items, 1, "after sharing it is his too")
	_, err = e.sandboxes.Stop(ctx, bob.ID, sandbox.ID)
	require.NoError(t, err)

	_, err = e.sandboxes.Stop(ctx, mallory.ID, sandbox.ID)
	require.ErrorIs(t, err, models.ErrNotFound, "outsiders stay out")
	_, _, err = e.projects.AddMember(ctx, mallory.ID, project.ID, *mallory.Email)
	require.ErrorIs(t, err, models.ErrNotFound, "outsiders cannot add themselves")

	projects, err := e.projects.List(ctx, bob.ID, models.Page{})
	require.NoError(t, err)
	require.Len(t, projects.Items, 1)
}

func TestTokenBucketAllowsBurstThenLimits(t *testing.T) {
	client := ratelimit.NewRedis(ratelimit.RedisConfig{
		Addr: getenv("REDIS_ADDR", defaultRedisAddr), DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, PoolSize: 4,
	})
	t.Cleanup(func() { _ = client.Close() })
	limiter := ratelimit.NewLimiter(client, breaker.New(100, time.Second))
	key := models.RateKey{Tenant: "test:" + uuid.NewString()}
	limit := models.RateLimit{PerSecond: 5, Burst: 3}

	for i := range limit.Burst {
		d, err := limiter.Allow(ctx, key, limit)
		require.NoError(t, err)
		require.True(t, d.Allowed, "request %d is inside the burst", i+1)
		require.Equal(t, limit.Burst-i-1, d.Remaining)
	}
	denied, err := limiter.Allow(ctx, key, limit)
	require.NoError(t, err)
	require.False(t, denied.Allowed)
	require.Positive(t, denied.RetryAfter)
	require.LessOrEqual(t, denied.RetryAfter, time.Second/time.Duration(limit.PerSecond)+time.Millisecond)

	time.Sleep(denied.RetryAfter + 50*time.Millisecond)
	refilled, err := limiter.Allow(ctx, key, limit)
	require.NoError(t, err)
	require.True(t, refilled.Allowed, "a token is back after RetryAfter")
}

func TestTokenBucketIsAtomicUnderConcurrency(t *testing.T) {
	client := ratelimit.NewRedis(ratelimit.RedisConfig{
		Addr: getenv("REDIS_ADDR", defaultRedisAddr), DialTimeout: time.Second,
		ReadTimeout: time.Second, WriteTimeout: time.Second, PoolSize: 40,
	})
	t.Cleanup(func() { _ = client.Close() })
	limiter := ratelimit.NewLimiter(client, breaker.New(100, time.Second))
	key := models.RateKey{Tenant: "test:" + uuid.NewString()}
	limit := models.RateLimit{PerSecond: 1, Burst: 10}

	const callers = 100
	var wg sync.WaitGroup
	var allowed atomic.Int32
	failures := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := limiter.Allow(ctx, key, limit)
			if err != nil {
				failures <- err
				return
			}
			if d.Allowed {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	close(failures)
	require.Empty(t, failures)

	// Burst plus at most the one token that refills while the test runs.
	require.InDelta(t, limit.Burst, int(allowed.Load()), 1)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func TestAnEmailAlreadyTakenDoesNotLockTheNewIdentityOut(t *testing.T) {
	e := setup(t, allowAll{})
	first := e.newUser(t)

	// A different identity whose claims yield the same email address.
	second, err := e.users.Resolve(ctx, models.Claims{Issuer: testIssuer, Subject: "opaque-subject", Email: *first.Email})

	require.NoError(t, err, "login must not fail with a database error")
	require.NotEqual(t, first.ID, second.ID)
	require.Nil(t, second.Email, "the email stays with its first owner")
	byEmail, err := e.store.GetUserByEmail(ctx, *first.Email)
	require.NoError(t, err)
	require.Equal(t, first.ID, byEmail.ID, "invites for that email still reach the original owner")
}

func TestConcurrentProjectCreationNeverExceedsTheCap(t *testing.T) {
	e := setup(t, allowAll{})
	owner := e.newUser(t)

	var wg sync.WaitGroup
	results := make(chan error, raceCallers)
	for range raceCallers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.projects.Create(ctx, owner.ID, "p")
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	created := 0
	for err := range results {
		var quota *models.ProjectQuotaError
		if err == nil {
			created++
		} else if !errorsAs(err, &quota) {
			require.NoError(t, err)
		}
	}
	require.Equal(t, usecase.MaxOwnedProjectsPerUser, created)
}

func TestOnlyOwnersCanInviteAndTheResponseShowsTheStoredRole(t *testing.T) {
	e := setup(t, allowAll{})
	owner, member, other := e.newUser(t), e.newUser(t), e.newUser(t)
	project, err := e.projects.Create(ctx, owner.ID, "team")
	require.NoError(t, err)
	_, _, err = e.projects.AddMember(ctx, owner.ID, project.ID, *member.Email)
	require.NoError(t, err)

	_, _, err = e.projects.AddMember(ctx, member.ID, project.ID, *other.Email)
	require.ErrorIs(t, err, models.ErrForbidden)

	got, created, err := e.projects.AddMember(ctx, owner.ID, project.ID, *owner.Email)
	require.NoError(t, err)
	require.False(t, created)
	require.Equal(t, models.RoleOwner, got.Role)
	require.False(t, got.AddedAt.IsZero(), "the stored membership is returned, timestamps included")
}
