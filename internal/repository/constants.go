package repository

import (
	"time"

	"github.com/buildoshi/e2b-api/pkg/retry"
)

// Postgres error codes that a retry of the whole transaction can fix.
// A lock timeout (55P03) is deliberately absent: the lock holder is probably
// still busy, so retrying would only multiply the wait. The caller is told to
// come back later instead.
const (
	pgSerializationFailure = "40001"
	pgDeadlockDetected     = "40P01"
	pgUniqueViolation      = "23505"

	usersEmailConstraint = "users_email_key"
)

// txRetry bounds how long a transaction is retried after a transient failure.
var txRetry = retry.Config{
	MaxAttempts: 3,
	BaseDelay:   20 * time.Millisecond,
	MaxDelay:    200 * time.Millisecond,
}
