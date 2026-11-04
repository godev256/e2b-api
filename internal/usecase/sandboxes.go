package usecase

import (
	"context"
	"errors"
	"regexp"

	"github.com/google/uuid"

	"github.com/buildoshi/e2b-api/internal/models"
	"github.com/buildoshi/e2b-api/pkg/idgen"
)

var templatePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)

// Sandboxes manages sandbox records. The lifecycle is simulated: a sandbox is
// running as soon as it is created and stopped when it is deleted.
type Sandboxes struct {
	store  Store
	access *AccessControl
}

func NewSandboxes(store Store, access *AccessControl) *Sandboxes {
	return &Sandboxes{store: store, access: access}
}

// Create starts a sandbox unless the project already runs as many as its plan
// allows. Check and insert share one transaction that holds the project row
// lock, so concurrent requests cannot both slip under the limit.
func (s *Sandboxes) Create(ctx context.Context, userID, projectID uuid.UUID, template string) (models.Sandbox, error) {
	if template == "" {
		template = DefaultSandboxTemplate
	}
	if len(template) > MaxTemplateLength || !templatePattern.MatchString(template) {
		return models.Sandbox{}, &models.InvalidInputError{Field: "template", Message: "must be lowercase letters, digits, dot, dash or underscore"}
	}
	grant, err := s.access.AuthorizeProject(ctx, userID, projectID)
	if err != nil {
		return models.Sandbox{}, err
	}

	var created models.Sandbox
	err = s.store.InTx(ctx, func(tx Repository) error {
		if err := tx.LockProject(ctx, projectID); err != nil {
			return err
		}
		running, err := tx.CountRunningSandboxes(ctx, projectID)
		if err != nil {
			return err
		}
		if running >= grant.Plan.MaxRunningSandboxes {
			return &models.SandboxQuotaError{Limit: grant.Plan.MaxRunningSandboxes, Running: running}
		}
		created, err = tx.CreateSandbox(ctx, models.Sandbox{
			ID:        idgen.New(),
			ProjectID: projectID,
			CreatedBy: userID,
			Template:  template,
			Status:    models.SandboxRunning,
		})
		return err
	})
	return created, err
}

// List returns a project's sandboxes, stopped ones included.
func (s *Sandboxes) List(ctx context.Context, userID, projectID uuid.UUID, page models.Page) (Paged[models.Sandbox], error) {
	if _, err := s.access.AuthorizeProject(ctx, userID, projectID); err != nil {
		return Paged[models.Sandbox]{}, err
	}
	return fetchPage(page,
		func(pg models.Page) ([]models.Sandbox, error) {
			return s.store.ListSandboxesByProject(ctx, projectID, pg)
		},
		func(sb models.Sandbox) uuid.UUID { return sb.ID },
	)
}

// Stop stops a sandbox. The route carries no project id, so the sandbox is
// looked up first and the caller is authorized against its project. Stopping
// a sandbox that is already stopped succeeds and changes nothing.
func (s *Sandboxes) Stop(ctx context.Context, userID, sandboxID uuid.UUID) (models.Sandbox, error) {
	sandbox, err := s.store.GetSandbox(ctx, sandboxID)
	if errors.Is(err, models.ErrNotFound) {
		return models.Sandbox{}, s.access.Miss(ctx, userID)
	}
	if err != nil {
		return models.Sandbox{}, err
	}
	if _, err := s.access.AuthorizeProject(ctx, userID, sandbox.ProjectID); err != nil {
		return models.Sandbox{}, err
	}
	if sandbox.Status == models.SandboxStopped {
		return sandbox, nil
	}

	stopped, err := s.store.StopSandbox(ctx, sandboxID)
	if errors.Is(err, models.ErrNotFound) {
		// Someone stopped it between our read and our write: same end state.
		return s.store.GetSandbox(ctx, sandboxID)
	}
	return stopped, err
}
