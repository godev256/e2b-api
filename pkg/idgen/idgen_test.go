package idgen_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/buildoshi/e2b-api/pkg/idgen"
)

const sampleSize = 1000

func TestNewReturnsVersion7IDs(t *testing.T) {
	require.EqualValues(t, 7, idgen.New().Version())
}

func TestNewIDsAreUniqueAndSortByCreationOrder(t *testing.T) {
	previous := idgen.New().String()
	for range sampleSize {
		next := idgen.New().String()
		require.Greater(t, next, previous, "later ids sort after earlier ones, which keeps index inserts append mostly")
		previous = next
	}
}
