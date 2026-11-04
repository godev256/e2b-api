// Package convert contains small generic helpers for converting values.
package convert

import (
	"cmp"
)

// Map applies fn to every element of in and returns the results.
// A nil input yields an empty, non-nil slice so JSON encodes it as [].
func Map[T, U any](in []T, fn func(T) U) []U {
	out := make([]U, 0, len(in))
	for _, v := range in {
		out = append(out, fn(v))
	}
	return out
}

// Clamp limits v to the closed interval [lo, hi].
func Clamp[T cmp.Ordered](v, lo, hi T) T {
	return min(max(v, lo), hi)
}
