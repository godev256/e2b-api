// Package idgen generates identifiers.
package idgen

import "github.com/google/uuid"

// New returns a time ordered UUIDv7.
//
// Time ordering keeps b-tree inserts append-only, which matters for large
// tables, while the random tail keeps identifiers impossible to guess.
func New() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}
