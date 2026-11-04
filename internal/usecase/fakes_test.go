package usecase_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/buildoshi/e2b-api/internal/models"
	"github.com/buildoshi/e2b-api/internal/usecase"
	"github.com/buildoshi/e2b-api/pkg/idgen"
)

// memStore is a small in-memory usecase.Store. Transactions run directly, which
// is enough for logic tests. Concurrency is covered by the integration tests.
type memStore struct {
	mu        sync.Mutex
	users     map[uuid.UUID]models.User
	plans     map[string]models.Plan
	access    map[[2]uuid.UUID]models.ProjectAccess // {project, user}
	sandboxes map[uuid.UUID]models.Sandbox
	order     []uuid.UUID

	upserts int
}

func newMemStore() *memStore {
	return &memStore{
		users: map[uuid.UUID]models.User{},
		plans: map[string]models.Plan{
			models.PlanFree: {Code: models.PlanFree, MaxRequestsPerSecond: 5, Burst: 10, MaxRunningSandboxes: 2},
		},
		access:    map[[2]uuid.UUID]models.ProjectAccess{},
		sandboxes: map[uuid.UUID]models.Sandbox{},
	}
}

func (s *memStore) grant(project, user uuid.UUID) { s.grantRole(project, user, models.RoleMember) }

func (s *memStore) grantOwner(project, user uuid.UUID) { s.grantRole(project, user, models.RoleOwner) }

func (s *memStore) grantRole(project, user uuid.UUID, role models.ProjectRole) {
	s.access[[2]uuid.UUID{project, user}] = models.ProjectAccess{ProjectID: project, PlanCode: models.PlanFree, Role: role}
}

// addUser registers a user that GetUserByEmail can find.
func (s *memStore) addUser(email string) models.User {
	u := models.User{ID: idgen.New(), Email: &email}
	s.users[u.ID] = u
	return u
}

func (s *memStore) InTx(_ context.Context, fn func(usecase.Repository) error) error { return fn(s) }

func (s *memStore) UpsertUser(_ context.Context, u models.User) (models.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.upserts++
	s.users[u.ID] = u
	return u, nil
}

func (s *memStore) GetUserByEmail(_ context.Context, email string) (models.User, error) {
	for _, u := range s.users {
		if u.Email != nil && *u.Email == email {
			return u, nil
		}
	}
	return models.User{}, models.ErrNotFound
}

func (s *memStore) LockUser(context.Context, uuid.UUID) error { return nil }

func (s *memStore) CountOwnedProjects(_ context.Context, user uuid.UUID) (int, error) {
	n := 0
	for key, a := range s.access {
		if key[1] == user && a.Role == models.RoleOwner {
			n++
		}
	}
	return n, nil
}

func (s *memStore) GetProjectMember(_ context.Context, project, user uuid.UUID) (models.ProjectMember, error) {
	a, ok := s.access[[2]uuid.UUID{project, user}]
	if !ok {
		return models.ProjectMember{}, models.ErrNotFound
	}
	return models.ProjectMember{UserID: user, Email: s.users[user].Email, Role: a.Role}, nil
}

func (s *memStore) GetPlan(_ context.Context, code string) (models.Plan, error) {
	if p, ok := s.plans[code]; ok {
		return p, nil
	}
	return models.Plan{}, models.ErrNotFound
}

func (s *memStore) CreateProject(_ context.Context, p models.Project) (models.Project, error) {
	return p, nil
}

func (s *memStore) AddProjectMember(_ context.Context, project, user, _ uuid.UUID, role models.ProjectRole) (bool, error) {
	key := [2]uuid.UUID{project, user}
	if _, exists := s.access[key]; exists {
		return false, nil
	}
	s.grantRole(project, user, role)
	return true, nil
}

func (s *memStore) ListProjectsForUser(context.Context, uuid.UUID, models.Page) ([]models.Project, error) {
	return nil, nil
}

func (s *memStore) ListProjectMembers(context.Context, uuid.UUID, models.Page) ([]models.ProjectMember, error) {
	return nil, nil
}

func (s *memStore) GetProjectAccess(_ context.Context, project, user uuid.UUID) (models.ProjectAccess, error) {
	if a, ok := s.access[[2]uuid.UUID{project, user}]; ok {
		return a, nil
	}
	return models.ProjectAccess{}, models.ErrNotFound
}

func (s *memStore) LockProject(context.Context, uuid.UUID) error { return nil }

func (s *memStore) CountRunningSandboxes(_ context.Context, project uuid.UUID) (int, error) {
	n := 0
	for _, sb := range s.sandboxes {
		if sb.ProjectID == project && sb.Status == models.SandboxRunning {
			n++
		}
	}
	return n, nil
}

func (s *memStore) CreateSandbox(_ context.Context, sb models.Sandbox) (models.Sandbox, error) {
	s.sandboxes[sb.ID] = sb
	s.order = append(s.order, sb.ID)
	return sb, nil
}

func (s *memStore) GetSandbox(_ context.Context, id uuid.UUID) (models.Sandbox, error) {
	if sb, ok := s.sandboxes[id]; ok {
		return sb, nil
	}
	return models.Sandbox{}, models.ErrNotFound
}

func (s *memStore) ListSandboxesByProject(_ context.Context, project uuid.UUID, page models.Page) ([]models.Sandbox, error) {
	var out []models.Sandbox
	for _, id := range s.order {
		sb := s.sandboxes[id]
		if sb.ProjectID == project && id.String() > page.After.String() && len(out) < page.Limit {
			out = append(out, sb)
		}
	}
	return out, nil
}

func (s *memStore) StopSandbox(_ context.Context, id uuid.UUID) (models.Sandbox, error) {
	sb, ok := s.sandboxes[id]
	if !ok || sb.Status != models.SandboxRunning {
		return models.Sandbox{}, models.ErrNotFound
	}
	sb.Status = models.SandboxStopped
	s.sandboxes[id] = sb
	return sb, nil
}

// stubLimiter returns a fixed decision or error.
type stubLimiter struct {
	decision models.RateDecision
	err      error
	calls    []models.RateKey
}

func (l *stubLimiter) Allow(_ context.Context, key models.RateKey, _ models.RateLimit) (models.RateDecision, error) {
	l.calls = append(l.calls, key)
	return l.decision, l.err
}

func allow() *stubLimiter { return &stubLimiter{decision: models.RateDecision{Allowed: true}} }

var errRedisDown = errors.New("redis down")

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
