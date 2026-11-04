package models

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrUnauthenticated means the caller did not present a valid token.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrNotFound is returned both for missing resources and for resources the
	// caller may not see, so identifiers cannot be probed.
	ErrNotFound = errors.New("not found")
	// ErrForbidden means the caller is a member but lacks the role for the action.
	ErrForbidden = errors.New("forbidden")
	// ErrAuthUnavailable means token verification failed because a dependency
	// is down, which says nothing about the token itself.
	ErrAuthUnavailable = errors.New("authentication is unavailable")
)

// InvalidInputError describes a request the caller must fix.
type InvalidInputError struct {
	Field   string
	Message string
}

func (e *InvalidInputError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Message)
}

// UserNotRegisteredError means an email does not belong to a known user yet.
type UserNotRegisteredError struct{ Email string }

func (e *UserNotRegisteredError) Error() string {
	return fmt.Sprintf("no user with email %q has signed in yet", e.Email)
}

// SandboxQuotaError means the project reached its running sandbox limit.
type SandboxQuotaError struct {
	Limit   int
	Running int
}

func (e *SandboxQuotaError) Error() string {
	return fmt.Sprintf("running sandbox quota exceeded: %d of %d in use", e.Running, e.Limit)
}

// ProjectQuotaError means the user owns as many projects as allowed.
type ProjectQuotaError struct {
	Limit int
	Owned int
}

func (e *ProjectQuotaError) Error() string {
	return fmt.Sprintf("project quota exceeded: %d of %d owned", e.Owned, e.Limit)
}

// RateLimitError means the caller is sending requests faster than the plan allows.
type RateLimitError struct {
	Decision RateDecision
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("rate limit exceeded, retry in %s", e.Decision.RetryAfter.Round(time.Millisecond))
}
