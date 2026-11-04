package httpapi

import (
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/buildoshi/e2b-api/internal/models"
)

// pathID parses the {id} path value. A malformed id is reported as not found,
// the same as an id that does not exist.
func pathID(r *http.Request) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return uuid.Nil, models.ErrNotFound
	}
	return id, nil
}

// pageFrom reads ?limit= and ?cursor=. Limits are clamped by the use case.
func pageFrom(r *http.Request) (models.Page, error) {
	q := r.URL.Query()
	var page models.Page

	if raw := q.Get(pageLimitParam); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			return page, &models.InvalidInputError{Field: pageLimitParam, Message: "must be a positive integer"}
		}
		page.Limit = limit
	}
	if raw := q.Get(pageCursorParam); raw != "" {
		cursor, err := uuid.Parse(raw)
		if err != nil {
			return page, &models.InvalidInputError{Field: pageCursorParam, Message: "is not a valid cursor"}
		}
		page.After = cursor
	}
	return page, nil
}
