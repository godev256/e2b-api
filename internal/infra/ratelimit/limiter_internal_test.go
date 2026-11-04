package ratelimit

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/buildoshi/e2b-api/internal/models"
)

func TestRedisKeysKeepATenantsBucketsInOneClusterSlot(t *testing.T) {
	main := redisKey(models.RateKey{Tenant: "project:42"})
	miss := redisKey(models.RateKey{Tenant: "user:7", Bucket: "miss"})

	require.Equal(t, "rl:{project:42}", main)
	require.Equal(t, "rl:{user:7}:miss", miss, "only the tenant is inside the braces, so it alone decides the slot")
}
