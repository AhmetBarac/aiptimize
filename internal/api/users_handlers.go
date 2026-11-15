package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/AhmetBarac/aiptimize/internal/auth"
	"github.com/AhmetBarac/aiptimize/internal/store"
)

// handleListUsers returns every account. Administrator only.
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.st.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not read users.", nil)
		return
	}
	out := make([]map[string]any, 0, len(users))
	for _, user := range users {
		out = append(out, map[string]any{
			"id":         user.ID,
			"username":   user.Username,
			"role":       user.Role,
			"created_at": user.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}

// handleCreateUser adds an account. Administrator only.
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	username := strings.TrimSpace(req.Username)
	if !usernamePattern.MatchString(username) {
		writeError(w, http.StatusUnprocessableEntity, "invalid_username",
			"Username must be 3-64 characters: letters, digits, dot, dash or underscore.", nil)
		return
	}
	if len(req.Password) < auth.MinPasswordLength {
		writeError(w, http.StatusUnprocessableEntity, "weak_password",
			"Password is too short.", nil)
		return
	}
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = store.RoleUser
	}
	if role != store.RoleAdmin && role != store.RoleUser {
		writeError(w, http.StatusUnprocessableEntity, "invalid_role", "Role must be admin or user.", nil)
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not hash the password.", nil)
		return
	}
	user, err := s.st.CreateUser(r.Context(), username, hash, role)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			writeError(w, http.StatusConflict, "username_taken", "That username is already in use.", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not create the user.", nil)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"user": map[string]any{"id": user.ID, "username": user.Username, "role": user.Role},
	})
}

// handleDeleteUser removes an account. Administrator only, and the last
// administrator can never be removed.
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "User id is required.", nil)
		return
	}
	users, err := s.st.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not read users.", nil)
		return
	}
	var target store.User
	found := false
	admins := 0
	for _, user := range users {
		if user.ID == id {
			target = user
			found = true
		}
		if user.IsAdmin() {
			admins++
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "user_not_found", "Unknown user.", nil)
		return
	}
	if target.IsAdmin() && admins <= 1 {
		writeError(w, http.StatusConflict, "last_admin", "The last administrator cannot be removed.", nil)
		return
	}
	if err := s.st.DeleteUser(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user_not_found", "Unknown user.", nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "Could not remove the user.", nil)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
