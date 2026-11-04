// Package ratelimit implements a distributed token bucket on Redis.
package ratelimit

import (
	"context"
	_ "embed"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/buildoshi/e2b-api/internal/models"
	"github.com/buildoshi/e2b-api/pkg/breaker"
)

//go:embed token_bucket.lua
var tokenBucketSource string

const requestCost = 1

// resultFields is the length of the array the Lua script returns.
const resultFields = 4

// redisKey builds the storage key. The braces are a Redis Cluster hash tag:
// only the part inside them is hashed, so every bucket of one tenant lands on
// the same shard and a multi key operation on a tenant stays possible.
func redisKey(key models.RateKey) string {
	if key.Bucket == "" {
		return "rl:{" + key.Tenant + "}"
	}
	return "rl:{" + key.Tenant + "}:" + key.Bucket
}

// Limiter decides rate limits with one atomic Redis call per request.
type Limiter struct {
	client  redis.Scripter
	script  *redis.Script
	breaker *breaker.Breaker
}

// NewLimiter wraps client. When Redis keeps failing, the breaker stops calling
// it for a while, so an outage costs nothing per request instead of a timeout.
func NewLimiter(client redis.Scripter, b *breaker.Breaker) *Limiter {
	return &Limiter{client: client, script: redis.NewScript(tokenBucketSource), breaker: b}
}

// Allow charges one request against the bucket stored under key. It is not
// retried on error: a retry after a lost reply would charge the bucket twice.
func (l *Limiter) Allow(ctx context.Context, key models.RateKey, limit models.RateLimit) (models.RateDecision, error) {
	var raw []int64
	err := l.breaker.Do(func() (err error) {
		raw, err = l.script.Run(ctx, l.client, []string{redisKey(key)}, limit.PerSecond, limit.Burst, requestCost).Int64Slice()
		return err
	})
	if err != nil {
		return models.RateDecision{}, fmt.Errorf("run token bucket script: %w", err)
	}
	if len(raw) != resultFields {
		return models.RateDecision{}, fmt.Errorf("token bucket script returned %d fields, want %d", len(raw), resultFields)
	}
	return models.RateDecision{
		Allowed:    raw[0] == 1,
		Limit:      limit.Burst,
		Remaining:  int(raw[1]),
		RetryAfter: time.Duration(raw[2]) * time.Millisecond,
		ResetAfter: time.Duration(raw[3]) * time.Millisecond,
	}, nil
}
