package httpapi

import (
	"net/http"

	"github.com/buildoshi/e2b-api/internal/models"
)

type createProjectRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request, user models.User) {
	var req createProjectRequest
	if err := decodeJSON(r, &req); err != nil {
		s.writeError(w, r, err)
		return
	}
	project, err := s.Projects.Create(r.Context(), user.ID, req.Name)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toProjectDTO(project))
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request, user models.User) {
	page, err := pageFrom(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	projects, err := s.Projects.List(r.Context(), user.ID, page)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toListDTO(projects, toProjectDTO))
}

type addMemberRequest struct {
	Email string `json:"email"`
}

// handleAddMember answers 201 for a new member and 200 when the user already
// belonged to the project, so the call can be repeated safely.
func (s *Server) handleAddMember(w http.ResponseWriter, r *http.Request, user models.User) {
	projectID, err := pathID(r)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var req addMemberRequest
	if err := decodeJSON(r, &req); err != nil {
		s.writeError(w, r, err)
		return
	}
	member, created, err := s.Projects.AddMember(r.Context(), user.ID, projectID, req.Email)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, toMemberDTO(member))
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request, user models.User) {
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
	members, err := s.Projects.ListMembers(r.Context(), user.ID, projectID, page)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, toListDTO(members, toMemberDTO))
}
