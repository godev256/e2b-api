package repository

import (
	"github.com/buildoshi/e2b-api/internal/models"
	"github.com/buildoshi/e2b-api/internal/repository/sqlcgen"
)

func toUser(u sqlcgen.User) models.User {
	return models.User{ID: u.ID, Issuer: u.Issuer, Subject: u.Subject, Email: u.Email, CreatedAt: u.CreatedAt}
}

func toPlan(p sqlcgen.Plan) models.Plan {
	return models.Plan{
		Code:                 p.Code,
		Name:                 p.Name,
		MaxRequestsPerSecond: int(p.MaxRequestsPerSecond),
		Burst:                int(p.Burst),
		MaxRunningSandboxes:  int(p.MaxRunningSandboxes),
	}
}

func toProject(p sqlcgen.Project) models.Project {
	return models.Project{ID: p.ID, Name: p.Name, PlanCode: p.PlanCode, CreatedBy: p.CreatedBy, CreatedAt: p.CreatedAt}
}

func toSandbox(s sqlcgen.Sandbox) models.Sandbox {
	return models.Sandbox{
		ID:        s.ID,
		ProjectID: s.ProjectID,
		CreatedBy: s.CreatedBy,
		Template:  s.Template,
		Status:    models.SandboxStatus(s.Status),
		CreatedAt: s.CreatedAt,
		StoppedAt: s.StoppedAt,
	}
}
