package ptr_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/buildoshi/e2b-api/pkg/ptr"
)

func TestToReturnsAPointerToACopy(t *testing.T) {
	original := "value"
	p := ptr.To(original)

	original = "changed"

	require.Equal(t, "value", *p, "the pointer does not follow later changes of the original")
	require.Equal(t, "changed", original)
}

func TestDerefReturnsTheValueOrZero(t *testing.T) {
	require.Equal(t, 7, ptr.Deref(ptr.To(7)))
	require.Equal(t, 0, ptr.Deref[int](nil))
	require.Equal(t, "", ptr.Deref[string](nil))
}
