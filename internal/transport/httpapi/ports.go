// Package http is the transport layer: it translates HTTP to use case calls
// and back, and contains no business rules.
package httpapi

import (
	"context"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/buildoshi/e2b-api/internal/models"
	"github.com/buildoshi/e2b-api/internal/usecase"
)

type Authenticator interface {
	Authenticate(ctx context.Context, rawToken string) (models.User, error)
}

type ProjectService interface {
	Create(ctx context.Context, userID uuid.UUID, name string) (models.Project, error)
	List(ctx context.Context, userID uuid.UUID, page models.Page) (usecase.Paged[models.Project], error)
	AddMember(ctx context.Context, actorID, projectID uuid.UUID, email string) (models.ProjectMember, bool, error)
	ListMembers(ctx context.Context, userID, projectID uuid.UUID, page models.Page) (usecase.Paged[models.ProjectMember], error)
}

type SandboxService interface {
	Create(ctx context.Context, userID, projectID uuid.UUID, template string) (models.Sandbox, error)
	List(ctx context.Context, userID, projectID uuid.UUID, page models.Page) (usecase.Paged[models.Sandbox], error)
	Stop(ctx context.Context, userID, sandboxID uuid.UUID) (models.Sandbox, error)
}

// OAuthFlow is the client side of the authorization code flow.
type OAuthFlow interface {
	NewFlow(state string) (verifier, authURL string)
	Exchange(ctx context.Context, code, verifier string) (*oauth2.Token, error)
}

// ReadinessCheck reports whether one dependency is usable.
type ReadinessCheck func(ctx context.Context) error
