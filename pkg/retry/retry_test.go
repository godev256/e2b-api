package retry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/buildoshi/e2b-api/pkg/retry"
)

var (
	errTransient = errors.New("transient")
	errFatal     = errors.New("fatal")

	testConfig = retry.Config{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 2 * time.Millisecond}
)

func isTransient(err error) bool { return errors.Is(err, errTransient) }

func TestDoRetriesTransientErrorsUntilSuccess(t *testing.T) {
	calls := 0
	err := retry.Do(context.Background(), testConfig, isTransient, func(context.Context) error {
		calls++
		if calls < testConfig.MaxAttempts {
			return errTransient
		}
		return nil
	})

	require.NoError(t, err)
	require.Equal(t, testConfig.MaxAttempts, calls)
}

func TestDoStopsOnNonRetryableError(t *testing.T) {
	calls := 0
	err := retry.Do(context.Background(), testConfig, isTransient, func(context.Context) error {
		calls++
		return errFatal
	})

	require.ErrorIs(t, err, errFatal)
	require.Equal(t, 1, calls)
}

func TestDoGivesUpAfterMaxAttempts(t *testing.T) {
	calls := 0
	err := retry.Do(context.Background(), testConfig, isTransient, func(context.Context) error {
		calls++
		return errTransient
	})

	require.ErrorIs(t, err, errTransient)
	require.Equal(t, testConfig.MaxAttempts, calls)
}

func TestDoStopsWhenContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := retry.Config{MaxAttempts: 5, BaseDelay: time.Hour, MaxDelay: time.Hour}
	err := retry.Do(ctx, cfg, isTransient, func(context.Context) error { return errTransient })

	require.ErrorIs(t, err, context.Canceled)
}
