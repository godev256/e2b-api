package usecase

import (
	"time"

	"github.com/buildoshi/e2b-api/internal/models"
)

const (
	// Pagination.
	DefaultPageSize = 50
	MaxPageSize     = 200

	// Input limits. MaxProjectNameLength mirrors the CHECK constraint on projects.name.
	MaxProjectNameLength = 100
	MaxEmailLength       = 254

	// DefaultSandboxTemplate is used when the caller does not pick a template.
	DefaultSandboxTemplate = "base"
	MaxTemplateLength      = 64

	// Plan assigned to a new project.
	NewProjectPlan = models.PlanFree

	// MaxOwnedProjectsPerUser bounds how many projects one user can own.
	// Quotas and rate limits are per project, so without this cap a user could
	// multiply their limits by creating projects.
	MaxOwnedProjectsPerUser = 10

	// In-process caches. Plans change rarely, identities never.
	cacheLoadTimeout    = 5 * time.Second
	planCacheTTL        = 30 * time.Second
	planCacheMaxEntries = 64
	userCacheTTL        = 5 * time.Minute
	userCacheMaxEntries = 10_000

	// Rate limit tenants.
	projectTenantFormat = "project:%s"
	userTenantFormat    = "user:%s"

	// missBucket is charged when a caller asks for something that does not
	// exist or is not theirs. Such requests cost a database read but never
	// reach a project bucket, so they need a bucket of their own.
	missBucket = "miss"
)

// unknownResourceLimit is deliberately small: honest clients rarely miss.
var unknownResourceLimit = models.RateLimit{PerSecond: 2, Burst: 10}
