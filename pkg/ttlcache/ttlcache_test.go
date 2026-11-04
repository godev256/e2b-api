package ttlcache_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/buildoshi/e2b-api/pkg/ttlcache"
)

const (
	testTTL         = time.Minute
	testMaxEntries  = 8
	testCallers     = 50
	testLoadTimeout = time.Second
)

func TestGetOrLoadCoalescesConcurrentLoads(t *testing.T) {
	cache := ttlcache.New[string, int](testTTL, testMaxEntries, testLoadTimeout)
	var loads atomic.Int32
	release := make(chan struct{})

	load := func(context.Context) (int, error) {
		loads.Add(1)
		<-release
		return 42, nil
	}

	var wg sync.WaitGroup
	results := make(chan int, testCallers)
	failures := make(chan error, testCallers)
	for range testCallers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := cache.GetOrLoad(context.Background(), "k", load)
			if err != nil {
				failures <- err
				return
			}
			results <- v
		}()
	}
	time.Sleep(50 * time.Millisecond) // let every caller reach the loader
	close(release)
	wg.Wait()
	close(results)
	close(failures)

	require.Empty(t, failures)
	for v := range results {
		require.Equal(t, 42, v)
	}

	require.Equal(t, int32(1), loads.Load())
}

func TestGetOrLoadServesFromCacheUntilExpiry(t *testing.T) {
	cache := ttlcache.New[string, int](10*time.Millisecond, testMaxEntries, testLoadTimeout)
	var loads atomic.Int32
	load := func(context.Context) (int, error) { return int(loads.Add(1)), nil }

	first, _ := cache.GetOrLoad(context.Background(), "k", load)
	second, _ := cache.GetOrLoad(context.Background(), "k", load)
	require.Equal(t, first, second)

	time.Sleep(20 * time.Millisecond)
	third, _ := cache.GetOrLoad(context.Background(), "k", load)
	require.NotEqual(t, first, third)
}

func TestGetOrLoadDoesNotCacheErrors(t *testing.T) {
	cache := ttlcache.New[string, int](testTTL, testMaxEntries, testLoadTimeout)
	boom := errors.New("boom")

	_, err := cache.GetOrLoad(context.Background(), "k", func(context.Context) (int, error) { return 0, boom })
	require.ErrorIs(t, err, boom)

	v, err := cache.GetOrLoad(context.Background(), "k", func(context.Context) (int, error) { return 7, nil })
	require.NoError(t, err)
	require.Equal(t, 7, v)
}

func TestGetOrLoadReturnsWhenOnlyTheWaiterIsCancelled(t *testing.T) {
	cache := ttlcache.New[string, int](testTTL, testMaxEntries, testLoadTimeout)
	release := make(chan struct{})
	defer close(release)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		_, err := cache.GetOrLoad(ctx, "k", func(context.Context) (int, error) { <-release; return 1, nil })
		done <- err
	}()
	cancel()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("a cancelled caller must not wait for a slow loader")
	}
}

func TestFullCacheEvictsOnlyASliceNotEverything(t *testing.T) {
	const capacity, reread = 100, 50
	cache := ttlcache.New[int, int](testTTL, capacity, testLoadTimeout)
	var loads atomic.Int32
	load := func(context.Context) (int, error) { return int(loads.Add(1)), nil }

	for i := 0; i <= capacity; i++ { // the last insert finds the cache full
		_, err := cache.GetOrLoad(context.Background(), i, load)
		require.NoError(t, err)
	}
	before := loads.Load()
	for i := 0; i < reread; i++ {
		_, _ = cache.GetOrLoad(context.Background(), i, load)
	}

	// Emptying the cache would reload all `reread` keys. Dropping a tenth of it
	// reloads only a fraction.
	require.Less(t, int(loads.Load()-before), reread/2, "eviction must not empty the cache")
}
