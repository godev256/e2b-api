// Package models contains the domain types shared by every layer.
package models

import (
	"time"

	"github.com/google/uuid"
)

// Claims is the verified identity extracted from an access token.
type Claims struct {
	Issuer  string
	Subject string
	// Email is empty when the provider does not supply one.
	Email string
}

type User struct {
	ID        uuid.UUID
	Issuer    string
	Subject   string
	Email     *string
	CreatedAt time.Time
}

// Plan is a named bundle of limits that a project is subject to.
type Plan struct {
	Code                 string
	Name                 string
	MaxRequestsPerSecond int
	Burst                int
	MaxRunningSandboxes  int
}

// PlanFree is the plan every new project starts on.
const PlanFree = "free"

type ProjectRole string

const (
	RoleOwner  ProjectRole = "owner"
	RoleMember ProjectRole = "member"
)

type Project struct {
	ID        uuid.UUID
	Name      string
	PlanCode  string
	CreatedBy uuid.UUID
	CreatedAt time.Time
}

// ProjectAccess proves that a user belongs to a project.
type ProjectAccess struct {
	ProjectID uuid.UUID
	PlanCode  string
	Role      ProjectRole
}

type ProjectMember struct {
	UserID  uuid.UUID
	Email   *string
	Role    ProjectRole
	AddedAt time.Time
}

type SandboxStatus string

const (
	SandboxRunning SandboxStatus = "running"
	SandboxStopped SandboxStatus = "stopped"
)

type Sandbox struct {
	ID        uuid.UUID
	ProjectID uuid.UUID
	CreatedBy uuid.UUID
	Template  string
	Status    SandboxStatus
	CreatedAt time.Time
	StoppedAt *time.Time
}

// Page describes one page of a keyset paginated list.
type Page struct {
	// After is the last id of the previous page. uuid.Nil starts from the beginning.
	After uuid.UUID
	Limit int
}

// RateKey names one token bucket. Tenant is whoever is being limited, such as
// "project:<id>" or "user:<id>". Bucket tells apart several buckets of one
// tenant, and is empty for the main one. How the key is stored is the business
// of the rate limiter implementation.
type RateKey struct {
	Tenant string
	Bucket string
}

// RateLimit is a token bucket specification.
type RateLimit struct {
	PerSecond int
	Burst     int
}

// RateDecision is the outcome of one rate limit check.
type RateDecision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
	ResetAfter time.Duration
}
