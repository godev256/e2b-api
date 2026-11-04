package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisConfig bounds every Redis call. Short timeouts matter here: the limiter
// sits on the hot path of every request and must never stall it.
type RedisConfig struct {
	Addr         string
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	PoolSize     int
	// Log receives go-redis's internal messages (connection errors and so on)
	// at debug level, instead of the library writing unstructured lines to stderr.
	Log *slog.Logger
}

type redisLogger struct{ log *slog.Logger }

func (l redisLogger) Printf(ctx context.Context, format string, v ...interface{}) {
	l.log.DebugContext(ctx, fmt.Sprintf(format, v...), slog.String("component", "go-redis"))
}

// disableRetries is go-redis's value for "never retry". Zero would not do:
// go-redis reads 0 as "use the default of 3".
const disableRetries = -1

// NewRedis creates a client with client side retries switched off. go-redis
// does retry some failures by default, including a connection closed in the
// middle of a command, and the command may already have run. For the rate
// limiter script that would charge a bucket twice.
func NewRedis(cfg RedisConfig) *redis.Client {
	if cfg.Log != nil {
		redis.SetLogger(redisLogger{log: cfg.Log})
	}
	return redis.NewClient(&redis.Options{
		MaxRetries:   disableRetries,
		Addr:         cfg.Addr,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		PoolSize:     cfg.PoolSize,
	})
}

// Ping reports whether Redis answers.
func Ping(ctx context.Context, client *redis.Client) error {
	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping redis: %w", err)
	}
	return nil
}
