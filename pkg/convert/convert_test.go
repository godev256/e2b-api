package convert_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/buildoshi/e2b-api/pkg/convert"
)

func TestMapConvertsEveryElement(t *testing.T) {
	require.Equal(t, []string{"1", "2", "3"}, convert.Map([]int{1, 2, 3}, strconv.Itoa))
}

func TestMapOfNilIsAnEmptySliceSoJSONShowsAnEmptyList(t *testing.T) {
	got := convert.Map[int, string](nil, strconv.Itoa)

	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestClampKeepsValuesInsideTheRange(t *testing.T) {
	require.Equal(t, 5, convert.Clamp(5, 1, 10))
	require.Equal(t, 1, convert.Clamp(-3, 1, 10))
	require.Equal(t, 10, convert.Clamp(99, 1, 10))
}
