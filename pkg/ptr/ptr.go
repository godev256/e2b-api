// Package ptr contains helpers for working with pointers.
package ptr

// To returns a pointer to a copy of v.
func To[T any](v T) *T {
	return &v
}

// Deref returns the value p points to, or the zero value when p is nil.
func Deref[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
