// Package usecase holds the business rules. It depends only on the interfaces
// declared here, which the infrastructure and repository layers implement.
package usecase

import (
	"context"

	"github.com/google/uuid"

	"github.com/buildoshi/e2b-api/internal/models"
)

// Repository is the persistence port. Every method that looks up one row
// returns models.ErrNotFound when it does not exist.
type Repository interface {
	UpsertUser(ctx context.Context, user models.User) (models.User, error)
	// LockUser takes a row lock held until the surrounding transaction ends.
	LockUser(ctx context.Context, userID uuid.UUID) error
	GetUserByEmail(ctx context.Context, email string) (models.User, error)

	GetPlan(ctx context.Context, code string) (models.Plan, error)

	CreateProject(ctx context.Context, project models.Project) (models.Project, error)
	// AddProjectMember reports whether a new membership was created.
	AddProjectMember(ctx context.Context, projectID, userID, addedBy uuid.UUID, role models.ProjectRole) (bool, error)
	ListProjectsForUser(ctx context.Context, userID uuid.UUID, page models.Page) ([]models.Project, error)
	CountOwnedProjects(ctx context.Context, userID uuid.UUID) (int, error)
	GetProjectMember(ctx context.Context, projectID, userID uuid.UUID) (models.ProjectMember, error)
	ListProjectMembers(ctx context.Context, projectID uuid.UUID, page models.Page) ([]models.ProjectMember, error)
	GetProjectAccess(ctx context.Context, projectID, userID uuid.UUID) (models.ProjectAccess, error)
	// LockProject takes a row lock held until the surrounding transaction ends.
	LockProject(ctx context.Context, projectID uuid.UUID) error

	CountRunningSandboxes(ctx context.Context, projectID uuid.UUID) (int, error)
	CreateSandbox(ctx context.Context, sandbox models.Sandbox) (models.Sandbox, error)
	GetSandbox(ctx context.Context, id uuid.UUID) (models.Sandbox, error)
	ListSandboxesByProject(ctx context.Context, projectID uuid.UUID, page models.Page) ([]models.Sandbox, error)
	// StopSandbox returns models.ErrNotFound when the sandbox is not running.
	StopSandbox(ctx context.Context, id uuid.UUID) (models.Sandbox, error)
}

// Store is a Repository that can also run several calls in one transaction.
type Store interface {
	Repository
	InTx(ctx context.Context, fn func(tx Repository) error) error
}

// TokenVerifier validates a bearer token and returns the identity inside it.
// It returns models.ErrUnauthenticated for any token it rejects.
type TokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (models.Claims, error)
}

// RateLimiter decides whether one more request fits into a token bucket.
type RateLimiter interface {
	Allow(ctx context.Context, key models.RateKey, limit models.RateLimit) (models.RateDecision, error)
}
