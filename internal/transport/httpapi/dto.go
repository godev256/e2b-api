package httpapi

import (
	"time"

	"github.com/buildoshi/e2b-api/internal/models"
	"github.com/buildoshi/e2b-api/internal/usecase"
	"github.com/buildoshi/e2b-api/pkg/convert"
)

// The wire format is defined here, separately from the domain types, so an
// internal change cannot silently change the public API.

type projectDTO struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Plan      string    `json:"plan"`
	CreatedAt time.Time `json:"created_at"`
}

type memberDTO struct {
	UserID  string     `json:"user_id"`
	Email   *string    `json:"email"`
	Role    string     `json:"role"`
	AddedAt *time.Time `json:"added_at,omitempty"`
}

type sandboxDTO struct {
	ID        string     `json:"id"`
	ProjectID string     `json:"project_id"`
	Template  string     `json:"template"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	StoppedAt *time.Time `json:"stopped_at"`
}

type listDTO[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

func toProjectDTO(p models.Project) projectDTO {
	return projectDTO{ID: p.ID.String(), Name: p.Name, Plan: p.PlanCode, CreatedAt: p.CreatedAt}
}

func toMemberDTO(m models.ProjectMember) memberDTO {
	dto := memberDTO{UserID: m.UserID.String(), Email: m.Email, Role: string(m.Role)}
	if !m.AddedAt.IsZero() {
		dto.AddedAt = &m.AddedAt
	}
	return dto
}

func toSandboxDTO(s models.Sandbox) sandboxDTO {
	return sandboxDTO{
		ID:        s.ID.String(),
		ProjectID: s.ProjectID.String(),
		Template:  s.Template,
		Status:    string(s.Status),
		CreatedAt: s.CreatedAt,
		StoppedAt: s.StoppedAt,
	}
}

func toListDTO[T, D any](paged usecase.Paged[T], toDTO func(T) D) listDTO[D] {
	out := listDTO[D]{Items: convert.Map(paged.Items, toDTO)}
	if paged.NextAfter != nil {
		cursor := paged.NextAfter.String()
		out.NextCursor = &cursor
	}
	return out
}
