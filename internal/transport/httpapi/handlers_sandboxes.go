package httpapi

import (
	"errors"
	"net/http"

	"github.com/buildoshi/e2b-api/internal/models"
)

type createSandboxRequest struct {
	Template string `json:"template"`
}

func (s *Server) handleCreateSandbox(w http.ResponseWriter, r *http.Request, user models.User) {
	projectID, err := pathID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req createSandboxRequest
	// The body is optional: an empty one means the default template.
	if r.ContentLength != 0 {
		if err := decodeJSON(r, &req); err != nil && !errors.Is(err, errEmptyBody) {
			s.writeError(w, r, err)
			return
		}
	}
	sandbox, err := s.Sandboxes.Create(r.Context(), user.ID, projectID, req.Template)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toSandboxDTO(sandbox))
}

func (s *Server) handleListSandboxes(w http.ResponseWriter, r *http.Request, user models.User) {
	projectID, err := pathID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	page, err := pageFrom(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	sandboxes, err := s.Sandboxes.List(r.Context(), user.ID, projectID, page)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toListDTO(sandboxes, toSandboxDTO))
}

func (s *Server) handleStopSandbox(w http.ResponseWriter, r *http.Request, user models.User) {
	sandboxID, err := pathID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	sandbox, err := s.Sandboxes.Stop(r.Context(), user.ID, sandboxID)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toSandboxDTO(sandbox))
}
