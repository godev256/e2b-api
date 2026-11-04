package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/buildoshi/e2b-api/internal/models"
	"github.com/buildoshi/e2b-api/pkg/idgen"
)

// Projects manages projects and who belongs to them.
type Projects struct {
	store  Store
	access *AccessControl
}

func NewProjects(store Store, access *AccessControl) *Projects {
	return &Projects{store: store, access: access}
}

// Create makes a project and its creator the owner, in one transaction.
func (p *Projects) Create(ctx context.Context, userID uuid.UUID, name string) (models.Project, error) {
	name, err := validateProjectName(name)
	if err != nil {
		return models.Project{}, err
	}
	if err := p.access.LimitUser(ctx, userID); err != nil {
		return models.Project{}, err
	}

	var created models.Project
	err = p.store.InTx(ctx, func(tx Repository) error {
		// Lock the user so two parallel creations cannot both pass the cap.
		if err := tx.LockUser(ctx, userID); err != nil {
			return err
		}
		owned, err := tx.CountOwnedProjects(ctx, userID)
		if err != nil {
			return err
		}
		if owned >= MaxOwnedProjectsPerUser {
			return &models.ProjectQuotaError{Limit: MaxOwnedProjectsPerUser, Owned: owned}
		}
		created, err = tx.CreateProject(ctx, models.Project{
			ID:        idgen.New(),
			Name:      name,
			PlanCode:  NewProjectPlan,
			CreatedBy: userID,
		})
		if err != nil {
			return err
		}
		_, err = tx.AddProjectMember(ctx, created.ID, userID, userID, models.RoleOwner)
		return err
	})
	return created, err
}

// List returns the projects the user belongs to.
func (p *Projects) List(ctx context.Context, userID uuid.UUID, page models.Page) (Paged[models.Project], error) {
	if err := p.access.LimitUser(ctx, userID); err != nil {
		return Paged[models.Project]{}, err
	}
	return fetchPage(page,
		func(pg models.Page) ([]models.Project, error) { return p.store.ListProjectsForUser(ctx, userID, pg) },
		func(pr models.Project) uuid.UUID { return pr.ID },
	)
}

// AddMember gives another user access to a project. From then on the project
// and its sandboxes are theirs too. Only owners can invite, and adding an
// existing member is a no-op that returns the stored membership.
func (p *Projects) AddMember(ctx context.Context, actorID, projectID uuid.UUID, email string) (member models.ProjectMember, created bool, err error) {
	normalized, ok := normalizeEmail(email)
	if !ok {
		return models.ProjectMember{}, false, &models.InvalidInputError{Field: "email", Message: "must be a valid email address"}
	}
	grant, err := p.access.AuthorizeProject(ctx, actorID, projectID)
	if err != nil {
		return models.ProjectMember{}, false, err
	}
	if grant.Access.Role != models.RoleOwner {
		return models.ProjectMember{}, false, models.ErrForbidden
	}

	target, err := p.store.GetUserByEmail(ctx, normalized)
	if errors.Is(err, models.ErrNotFound) {
		return models.ProjectMember{}, false, &models.UserNotRegisteredError{Email: normalized}
	}
	if err != nil {
		return models.ProjectMember{}, false, err
	}

	created, err = p.store.AddProjectMember(ctx, projectID, target.ID, actorID, models.RoleMember)
	if err != nil {
		return models.ProjectMember{}, false, err
	}
	member, err = p.store.GetProjectMember(ctx, projectID, target.ID)
	return member, created, err
}

// ListMembers returns the people who share a project.
func (p *Projects) ListMembers(ctx context.Context, userID, projectID uuid.UUID, page models.Page) (Paged[models.ProjectMember], error) {
	if _, err := p.access.AuthorizeProject(ctx, userID, projectID); err != nil {
		return Paged[models.ProjectMember]{}, err
	}
	return fetchPage(page,
		func(pg models.Page) ([]models.ProjectMember, error) {
			return p.store.ListProjectMembers(ctx, projectID, pg)
		},
		func(m models.ProjectMember) uuid.UUID { return m.UserID },
	)
}

// validateProjectName trims the name and rejects anything that is not plain
// printable text. Control characters would break logs and terminals, and
// bidirectional overrides can make one name look like another.
func validateProjectName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	invalid := func(msg string) error { return &models.InvalidInputError{Field: "name", Message: msg} }

	if n := utf8.RuneCountInString(name); n == 0 || n > MaxProjectNameLength {
		return "", invalid(fmt.Sprintf("must be 1 to %d characters", MaxProjectNameLength))
	}
	for _, r := range name {
		if unicode.IsControl(r) || isBidiControl(r) || r == utf8.RuneError {
			return "", invalid("must not contain control or direction override characters")
		}
	}
	return name, nil
}

// Unicode bidirectional control characters, written as code points so the
// source itself contains no invisible characters.
const (
	bidiEmbeddingStart = 0x202A // LRE, RLE, PDF, LRO, RLO span 0x202A to 0x202E
	bidiEmbeddingEnd   = 0x202E
	bidiIsolateStart   = 0x2066 // LRI, RLI, FSI, PDI span 0x2066 to 0x2069
	bidiIsolateEnd     = 0x2069
	leftToRightMark    = 0x200E
	rightToLeftMark    = 0x200F
)

// isBidiControl reports Unicode bidirectional embeddings, overrides, isolates and marks.
func isBidiControl(r rune) bool {
	return (r >= bidiEmbeddingStart && r <= bidiEmbeddingEnd) ||
		(r >= bidiIsolateStart && r <= bidiIsolateEnd) ||
		r == leftToRightMark || r == rightToLeftMark
}
