package breaker_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/buildoshi/e2b-api/pkg/breaker"
)

const (
	testThreshold = 3
	testCooldown  = 30 * time.Millisecond
)

var errDown = errors.New("down")

func fail() error    { return errDown }
func succeed() error { return nil }

func TestOpensAfterConsecutiveFailuresAndSkipsCalls(t *testing.T) {
	b := breaker.New(testThreshold, time.Hour)
	for range testThreshold {
		require.ErrorIs(t, b.Do(fail), errDown)
	}

	called := false
	err := b.Do(func() error { called = true; return nil })

	require.ErrorIs(t, err, breaker.ErrOpen)
	require.False(t, called, "an open breaker must not call the dependency")
}

func TestSuccessResetsTheFailureCount(t *testing.T) {
	b := breaker.New(testThreshold, time.Hour)
	for range testThreshold - 1 {
		_ = b.Do(fail)
	}
	require.NoError(t, b.Do(succeed))
	for range testThreshold - 1 {
		_ = b.Do(fail)
	}

	require.NoError(t, b.Do(succeed), "failures were not consecutive, so the breaker stays closed")
}

func TestClosesAgainAfterCooldownAndReopensOnTrialFailure(t *testing.T) {
	b := breaker.New(testThreshold, testCooldown)
	for range testThreshold {
		_ = b.Do(fail)
	}
	time.Sleep(2 * testCooldown)

	require.ErrorIs(t, b.Do(fail), errDown, "trial call is allowed after the cooldown")
	require.ErrorIs(t, b.Do(succeed), breaker.ErrOpen, "one failed trial reopens it")
}
