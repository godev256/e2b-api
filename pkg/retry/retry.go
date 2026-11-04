// Package retry runs an operation again when it fails with a transient error.
package retry

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"
)

// Config controls how often and how long Do waits between attempts.
type Config struct {
	// MaxAttempts is the total number of calls, including the first one.
	MaxAttempts int
	// BaseDelay is the delay before the second attempt.
	BaseDelay time.Duration
	// MaxDelay caps the exponential growth of the delay.
	MaxDelay time.Duration
}

const backoffFactor = 2

// Do calls fn until it succeeds, returns an error that retryable rejects,
// the attempts are used up, or ctx is done. The delay grows exponentially and
// uses full jitter so that many callers do not retry in lockstep.
func Do(ctx context.Context, cfg Config, retryable func(error) bool, fn func(context.Context) error) error {
	var err error
	delay := cfg.BaseDelay
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		if err = fn(ctx); err == nil {
			return nil
		}
		if !retryable(err) || attempt == cfg.MaxAttempts {
			return err
		}
		if waitErr := sleep(ctx, jitter(delay)); waitErr != nil {
			return errors.Join(err, waitErr)
		}
		delay = min(delay*backoffFactor, cfg.MaxDelay)
	}
	return err
}

// Value is Do for functions that return a value.
func Value[T any](ctx context.Context, cfg Config, retryable func(error) bool, fn func(context.Context) (T, error)) (T, error) {
	var out T
	err := Do(ctx, cfg, retryable, func(ctx context.Context) error {
		var callErr error
		out, callErr = fn(ctx)
		return callErr
	})
	return out, err
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(d)) + 1) //nolint:gosec // jitter does not need a cryptographic source
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
