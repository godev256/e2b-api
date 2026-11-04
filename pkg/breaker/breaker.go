// Package breaker is a minimal circuit breaker. After a run of consecutive
// failures it stops calling a dependency for a cooldown, so a dead dependency
// costs nothing instead of a timeout on every request.
package breaker

import (
	"errors"
	"sync"
	"time"
)

// ErrOpen is returned while the breaker refuses calls.
var ErrOpen = errors.New("circuit breaker is open")

// Breaker is safe for concurrent use. The zero value is not usable, call New.
type Breaker struct {
	threshold int
	cooldown  time.Duration
	now       func() time.Time

	mu        sync.Mutex
	failures  int
	openUntil time.Time
}

// New opens the breaker after threshold consecutive failures and keeps it
// open for cooldown. Afterwards calls flow again (a trial), and one more
// failure reopens it immediately.
func New(threshold int, cooldown time.Duration) *Breaker {
	return &Breaker{threshold: threshold, cooldown: cooldown, now: time.Now}
}

// Do runs fn unless the breaker is open.
func (b *Breaker) Do(fn func() error) error {
	if !b.allow() {
		return ErrOpen
	}
	err := fn()
	b.record(err)
	return err
}

func (b *Breaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.now().Before(b.openUntil)
}

func (b *Breaker) record(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err == nil {
		b.failures = 0
		return
	}
	b.failures++
	if b.failures >= b.threshold {
		b.openUntil = b.now().Add(b.cooldown)
		// Stay one failure away from reopening during the trial after cooldown.
		b.failures = b.threshold - 1
	}
}
