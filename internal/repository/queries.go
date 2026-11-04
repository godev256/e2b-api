package repository

import (
	"context"
	"errors"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/buildoshi/e2b-api/internal/models"
	"github.com/buildoshi/e2b-api/internal/repository/sqlcgen"
	"github.com/buildoshi/e2b-api/pkg/convert"
)

// queries adapts the sqlc generated code to the domain types. It works on a
// pool or on a transaction, depending on what the Queries value wraps.
type queries struct {
	q *sqlcgen.Queries
}

// limit32 converts a page size for sqlc. Page sizes are bounded by the use case
// layer, the clamp only makes the conversion provably safe.
func limit32(n int) int32 {
	return int32(convert.Clamp(n, 0, math.MaxInt32)) //nolint:gosec // clamped to the int32 range on the line above
}

// wrap turns a driver error into a domain error where one exists.
func wrap[T any](value T, err error) (T, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return value, models.ErrNotFound
	}
	return value, err
}

// UpsertUser creates the user or refreshes it. If the email already belongs to
// another identity, the user is stored without it: the email is only a lookup
// convenience, and failing the login would lock the person out, while handing
// them someone else's email would be worse.
func (r queries) UpsertUser(ctx context.Context, u models.User) (models.User, error) {
	params := sqlcgen.UpsertUserParams{ID: u.ID, Issuer: u.Issuer, Subject: u.Subject, Email: u.Email}
	row, err := r.q.UpsertUser(ctx, params)
	if isUniqueViolation(err, usersEmailConstraint) {
		params.Email = nil
		row, err = r.q.UpsertUser(ctx, params)
	}
	return toUser(row), err
}

func (r queries) LockUser(ctx context.Context, userID uuid.UUID) error {
	_, err := wrap(r.q.LockUser(ctx, userID))
	return err
}

func (r queries) CountOwnedProjects(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := r.q.CountOwnedProjects(ctx, userID)
	return int(n), err
}

func (r queries) GetProjectMember(ctx context.Context, projectID, userID uuid.UUID) (models.ProjectMember, error) {
	row, err := wrap(r.q.GetProjectMember(ctx, sqlcgen.GetProjectMemberParams{ProjectID: projectID, UserID: userID}))
	return models.ProjectMember{UserID: row.UserID, Email: row.Email, Role: models.ProjectRole(row.Role), AddedAt: row.AddedAt}, err
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation && pgErr.ConstraintName == constraint
}

func (r queries) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
	row, err := wrap(r.q.GetUserByEmail(ctx, &email))
	return toUser(row), err
}

func (r queries) GetPlan(ctx context.Context, code string) (models.Plan, error) {
	row, err := wrap(r.q.GetPlan(ctx, code))
	return toPlan(row), err
}

func (r queries) CreateProject(ctx context.Context, p models.Project) (models.Project, error) {
	row, err := wrap(r.q.CreateProject(ctx, sqlcgen.CreateProjectParams{
		ID: p.ID, Name: p.Name, PlanCode: p.PlanCode, CreatedBy: p.CreatedBy,
	}))
	return toProject(row), err
}

func (r queries) AddProjectMember(ctx context.Context, projectID, userID, addedBy uuid.UUID, role models.ProjectRole) (bool, error) {
	inserted, err := r.q.AddProjectMember(ctx, sqlcgen.AddProjectMemberParams{
		ProjectID: projectID, UserID: userID, Role: sqlcgen.ProjectRole(role), AddedBy: addedBy,
	})
	return inserted > 0, err
}

func (r queries) ListProjectsForUser(ctx context.Context, userID uuid.UUID, page models.Page) ([]models.Project, error) {
	rows, err := r.q.ListProjectsForUser(ctx, sqlcgen.ListProjectsForUserParams{
		UserID: userID, ID: page.After, Limit: limit32(page.Limit),
	})
	return convert.Map(rows, toProject), err
}

func (r queries) ListProjectMembers(ctx context.Context, projectID uuid.UUID, page models.Page) ([]models.ProjectMember, error) {
	rows, err := r.q.ListProjectMembers(ctx, sqlcgen.ListProjectMembersParams{
		ProjectID: projectID, UserID: page.After, Limit: limit32(page.Limit),
	})
	return convert.Map(rows, func(m sqlcgen.ListProjectMembersRow) models.ProjectMember {
		return models.ProjectMember{UserID: m.UserID, Email: m.Email, Role: models.ProjectRole(m.Role), AddedAt: m.AddedAt}
	}), err
}

func (r queries) GetProjectAccess(ctx context.Context, projectID, userID uuid.UUID) (models.ProjectAccess, error) {
	row, err := wrap(r.q.GetProjectAccess(ctx, sqlcgen.GetProjectAccessParams{ID: projectID, UserID: userID}))
	return models.ProjectAccess{ProjectID: row.ProjectID, PlanCode: row.PlanCode, Role: models.ProjectRole(row.Role)}, err
}

func (r queries) LockProject(ctx context.Context, projectID uuid.UUID) error {
	_, err := wrap(r.q.LockProject(ctx, projectID))
	return err
}

func (r queries) CountRunningSandboxes(ctx context.Context, projectID uuid.UUID) (int, error) {
	n, err := r.q.CountRunningSandboxes(ctx, projectID)
	return int(n), err
}

func (r queries) CreateSandbox(ctx context.Context, s models.Sandbox) (models.Sandbox, error) {
	row, err := wrap(r.q.CreateSandbox(ctx, sqlcgen.CreateSandboxParams{
		ID: s.ID, ProjectID: s.ProjectID, CreatedBy: s.CreatedBy, Template: s.Template,
	}))
	return toSandbox(row), err
}

func (r queries) GetSandbox(ctx context.Context, id uuid.UUID) (models.Sandbox, error) {
	row, err := wrap(r.q.GetSandbox(ctx, id))
	return toSandbox(row), err
}

func (r queries) ListSandboxesByProject(ctx context.Context, projectID uuid.UUID, page models.Page) ([]models.Sandbox, error) {
	rows, err := r.q.ListSandboxesByProject(ctx, sqlcgen.ListSandboxesByProjectParams{
		ProjectID: projectID, ID: page.After, Limit: limit32(page.Limit),
	})
	return convert.Map(rows, toSandbox), err
}

func (r queries) StopSandbox(ctx context.Context, id uuid.UUID) (models.Sandbox, error) {
	row, err := wrap(r.q.StopSandbox(ctx, id))
	return toSandbox(row), err
}
