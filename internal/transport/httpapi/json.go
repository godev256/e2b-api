package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/buildoshi/e2b-api/internal/models"
)

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	// The status line is already sent, so an encode error cannot be reported.
	_ = json.NewEncoder(w).Encode(body)
}

// decodeJSON reads exactly one JSON object and rejects unknown fields, so a
// typo in a field name is an error instead of being silently ignored.
func decodeJSON(r *http.Request, dst any) error {
	if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mediaType != "application/json" {
		return errUnsupportedMedia
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return errEmptyBody
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return err
		}
		return &models.InvalidInputError{Field: "body", Message: "must be a valid JSON object"}
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return &models.InvalidInputError{Field: "body", Message: "must contain a single JSON object"}
	}
	return nil
}

var (
	errUnsupportedMedia = errors.New("content type must be application/json")
	errEmptyBody        = &models.InvalidInputError{Field: "body", Message: "must not be empty"}
)
