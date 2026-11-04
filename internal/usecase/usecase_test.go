package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/buildoshi/e2b-api/internal/models"
	"github.com/buildoshi/e2b-api/internal/usecase"
	"github.com/buildoshi/e2b-api/pkg/idgen"
)

var ctx = context.Background()

func TestAccessControlHidesProjectsFromNonMembers(t *testing.T) {
	store := newMemStore()
	limiter := allow()
	access := usecase.NewAccessControl(store, limiter, discardLogger())

	_, err := access.AuthorizeProject(ctx, idgen.New(), idgen.New())

	require.ErrorIs(t, err, models.ErrNotFound)
	require.Len(t, limiter.calls, 1)
	require.Equal(t, "miss", limiter.calls[0].Bucket, "a stranger is charged to the miss bucket, never to the project's")
}

func TestRepeatedMissesAreRateLimited(t *testing.T) {
	store := newMemStore()
	limiter := &stubLimiter{decision: models.RateDecision{Allowed: false, RetryAfter: time.Second}}
	access := usecase.NewAccessControl(store, limiter, discardLogger())

	_, err := access.AuthorizeProject(ctx, idgen.New(), idgen.New())

	var rateErr *models.RateLimitError
	require.ErrorAs(t, err, &rateErr, "probing random ids must not be a free way to load the database")
}

func TestAccessControlRejectsWhenRateLimited(t *testing.T) {
	store := newMemStore()
	project, user := idgen.New(), idgen.New()
	store.grant(project, user)
	limiter := &stubLimiter{decision: models.RateDecision{Allowed: false, RetryAfter: 300 * time.Millisecond}}
	access := usecase.NewAccessControl(store, limiter, discardLogger())

	_, err := access.AuthorizeProject(ctx, user, project)

	var rateErr *models.RateLimitError
	require.ErrorAs(t, err, &rateErr)
	require.Equal(t, 300*time.Millisecond, rateErr.Decision.RetryAfter)
}

func TestAccessControlFailsOpenWhenLimiterIsDown(t *testing.T) {
	store := newMemStore()
	project, user := idgen.New(), idgen.New()
	store.grant(project, user)
	access := usecase.NewAccessControl(store, &stubLimiter{err: errRedisDown}, discardLogger())

	grant, err := access.AuthorizeProject(ctx, user, project)

	require.NoError(t, err)
	require.Equal(t, project, grant.Access.ProjectID)
}

func TestSandboxCreateEnforcesPlanQuota(t *testing.T) {
	store := newMemStore()
	project, user := idgen.New(), idgen.New()
	store.grant(project, user)
	sandboxes := usecase.NewSandboxes(store, usecase.NewAccessControl(store, allow(), discardLogger()))

	for range store.plans[models.PlanFree].MaxRunningSandboxes {
		_, err := sandboxes.Create(ctx, user, project, "")
		require.NoError(t, err)
	}
	_, err := sandboxes.Create(ctx, user, project, "")

	var quota *models.SandboxQuotaError
	require.ErrorAs(t, err, &quota)
	require.Equal(t, 2, quota.Limit)
	require.Equal(t, 2, quota.Running)
}

func TestSandboxCreateValidatesTemplate(t *testing.T) {
	store := newMemStore()
	project, user := idgen.New(), idgen.New()
	store.grant(project, user)
	sandboxes := usecase.NewSandboxes(store, usecase.NewAccessControl(store, allow(), discardLogger()))

	for _, bad := range []string{"Has Space", "../etc", "UPPER", "-leading"} {
		_, err := sandboxes.Create(ctx, user, project, bad)
		var invalid *models.InvalidInputError
		require.ErrorAs(t, err, &invalid, bad)
	}
}

func TestSandboxStopIsIdempotentAndScopedToMembers(t *testing.T) {
	store := newMemStore()
	project, owner, stranger := idgen.New(), idgen.New(), idgen.New()
	store.grant(project, owner)
	sandboxes := usecase.NewSandboxes(store, usecase.NewAccessControl(store, allow(), discardLogger()))
	created, err := sandboxes.Create(ctx, owner, project, "")
	require.NoError(t, err)

	_, err = sandboxes.Stop(ctx, stranger, created.ID)
	require.ErrorIs(t, err, models.ErrNotFound, "non-members see the same answer as for a missing sandbox")

	for range 2 {
		stopped, err := sandboxes.Stop(ctx, owner, created.ID)
		require.NoError(t, err)
		require.Equal(t, models.SandboxStopped, stopped.Status)
	}

	_, err = sandboxes.Stop(ctx, owner, uuid.New())
	require.ErrorIs(t, err, models.ErrNotFound)
}

func TestListPaginatesWithKeysetCursor(t *testing.T) {
	store := newMemStore()
	project, user := idgen.New(), idgen.New()
	store.grant(project, user)
	store.plans[models.PlanFree] = models.Plan{Code: models.PlanFree, MaxRunningSandboxes: 10}
	sandboxes := usecase.NewSandboxes(store, usecase.NewAccessControl(store, allow(), discardLogger()))
	for range 5 {
		_, err := sandboxes.Create(ctx, user, project, "")
		require.NoError(t, err)
	}

	var seen int
	page := models.Page{Limit: 2}
	for pages := 0; ; pages++ {
		require.Less(t, pages, 10, "pagination must terminate")
		result, err := sandboxes.List(ctx, user, project, page)
		require.NoError(t, err)
		seen += len(result.Items)
		if result.NextAfter == nil {
			break
		}
		page.After = *result.NextAfter
	}

	require.Equal(t, 5, seen)
}

func TestUsersResolveCreatesOnceThenServesFromCache(t *testing.T) {
	store := newMemStore()
	users := usecase.NewUsers(store)
	claims := models.Claims{Issuer: "http://hydra", Subject: "Foo@Bar.com"}

	first, err := users.Resolve(ctx, claims)
	require.NoError(t, err)
	second, err := users.Resolve(ctx, claims)
	require.NoError(t, err)

	require.Equal(t, first.ID, second.ID)
	require.Equal(t, 1, store.upserts, "a warm identity must not touch the database")
	require.NotNil(t, first.Email)
	require.Equal(t, "foo@bar.com", *first.Email, "an email-shaped subject is normalized to lowercase")
}

func TestUsersResolveLeavesEmailEmptyForOpaqueSubjects(t *testing.T) {
	users := usecase.NewUsers(newMemStore())

	user, err := users.Resolve(ctx, models.Claims{Issuer: "http://hydra", Subject: "8f14e45f-opaque"})

	require.NoError(t, err)
	require.Nil(t, user.Email)
}

func TestProjectsCreateValidatesName(t *testing.T) {
	store := newMemStore()
	projects := usecase.NewProjects(store, usecase.NewAccessControl(store, allow(), discardLogger()))

	for _, name := range []string{"", "   ", string(make([]rune, usecase.MaxProjectNameLength+1))} {
		_, err := projects.Create(ctx, idgen.New(), name)
		var invalid *models.InvalidInputError
		require.True(t, errors.As(err, &invalid), "name %q", name)
	}
}

func TestAddMemberRequiresOwnerRole(t *testing.T) {
	store := newMemStore()
	project, owner, member := idgen.New(), idgen.New(), idgen.New()
	store.grantOwner(project, owner)
	store.grant(project, member)
	invitee := store.addUser("new@example.com")
	projects := usecase.NewProjects(store, usecase.NewAccessControl(store, allow(), discardLogger()))

	_, _, err := projects.AddMember(ctx, member, project, "new@example.com")
	require.ErrorIs(t, err, models.ErrForbidden, "a plain member cannot invite")

	got, created, err := projects.AddMember(ctx, owner, project, "new@example.com")
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, invitee.ID, got.UserID)
	require.Equal(t, models.RoleMember, got.Role)
}

func TestAddMemberReturnsTheStoredRoleForExistingMembers(t *testing.T) {
	store := newMemStore()
	project := idgen.New()
	owner := store.addUser("owner@example.com")
	store.grantOwner(project, owner.ID)
	projects := usecase.NewProjects(store, usecase.NewAccessControl(store, allow(), discardLogger()))

	got, created, err := projects.AddMember(ctx, owner.ID, project, "OWNER@example.com")

	require.NoError(t, err)
	require.False(t, created, "adding an existing member is a no-op")
	require.Equal(t, models.RoleOwner, got.Role, "the response must show the real role, not a default")
}

func TestAddMemberReportsUnknownEmails(t *testing.T) {
	store := newMemStore()
	project, owner := idgen.New(), idgen.New()
	store.grantOwner(project, owner)
	projects := usecase.NewProjects(store, usecase.NewAccessControl(store, allow(), discardLogger()))

	_, _, err := projects.AddMember(ctx, owner, project, "ghost@example.com")

	var unknown *models.UserNotRegisteredError
	require.ErrorAs(t, err, &unknown)
}

func TestProjectCreateEnforcesPerUserCap(t *testing.T) {
	store := newMemStore()
	user := idgen.New()
	for range usecase.MaxOwnedProjectsPerUser {
		store.grantOwner(idgen.New(), user)
	}
	projects := usecase.NewProjects(store, usecase.NewAccessControl(store, allow(), discardLogger()))

	_, err := projects.Create(ctx, user, "one too many")

	var quota *models.ProjectQuotaError
	require.ErrorAs(t, err, &quota)
	require.Equal(t, usecase.MaxOwnedProjectsPerUser, quota.Limit)
}

func TestProjectNameRejectsUnsafeCharacters(t *testing.T) {
	store := newMemStore()
	projects := usecase.NewProjects(store, usecase.NewAccessControl(store, allow(), discardLogger()))

	for name, bad := range map[string]string{
		"nul byte":        "nul\x00byte",
		"newline":         "line\nbreak",
		"escape":          "esc\x1b[31m",
		"right to left":   "gpj\u202Efdp.exe",
		"isolate":         "a\u2066b",
		"invalid utf8":    "bad\xffbyte",
		"only whitespace": " \t ",
	} {
		_, err := projects.Create(ctx, idgen.New(), bad)
		var invalid *models.InvalidInputError
		require.ErrorAs(t, err, &invalid, name)
	}

	created, err := projects.Create(ctx, idgen.New(), "  Ünïcode проект 项目  ")
	require.NoError(t, err)
	require.Equal(t, "Ünïcode проект 项目", created.Name, "ordinary unicode is fine and is trimmed")
}

func TestStopOfMissingSandboxIsChargedAsAMiss(t *testing.T) {
	store := newMemStore()
	limiter := allow()
	sandboxes := usecase.NewSandboxes(store, usecase.NewAccessControl(store, limiter, discardLogger()))

	_, err := sandboxes.Stop(ctx, idgen.New(), idgen.New())

	require.ErrorIs(t, err, models.ErrNotFound)
	require.Len(t, limiter.calls, 1)
	require.Equal(t, "miss", limiter.calls[0].Bucket)
}
