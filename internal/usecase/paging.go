package usecase

import (
	"github.com/google/uuid"

	"github.com/buildoshi/e2b-api/internal/models"
	"github.com/buildoshi/e2b-api/pkg/convert"
)

// Paged is one page of a keyset paginated list.
type Paged[T any] struct {
	Items []T
	// NextAfter is the cursor for the following page, nil on the last page.
	NextAfter *uuid.UUID
}

// normalizePage fills in defaults and enforces the maximum page size.
func normalizePage(page models.Page) models.Page {
	if page.Limit <= 0 {
		page.Limit = DefaultPageSize
	}
	page.Limit = convert.Clamp(page.Limit, 1, MaxPageSize)
	return page
}

// fetchPage asks the repository for limit+1 rows. The extra row only proves
// that another page exists and is never returned.
func fetchPage[T any](
	page models.Page,
	fetch func(models.Page) ([]T, error),
	idOf func(T) uuid.UUID,
) (Paged[T], error) {
	page = normalizePage(page)
	rows, err := fetch(models.Page{After: page.After, Limit: page.Limit + 1})
	if err != nil {
		return Paged[T]{}, err
	}
	if len(rows) <= page.Limit {
		return Paged[T]{Items: rows}, nil
	}
	rows = rows[:page.Limit]
	next := idOf(rows[len(rows)-1])
	return Paged[T]{Items: rows, NextAfter: &next}, nil
}
