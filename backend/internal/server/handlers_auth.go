package server

import (
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"copilot-local-api/internal/store"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	u, ok := s.store.UserByEmail(strings.TrimSpace(strings.ToLower(body.Email)))
	if !ok || !s.store.CheckPassword(u, body.Password) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "message": "Invalid credentials"})
		return
	}
	tok, err := s.store.NewSession(u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "Could not create session"})
		return
	}
	s.recordAudit(u.ID, "login", "session", u.ID, "Logged in", nil)
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"user":         userPublic(u),
			"access_token": tok,
			"token_type":   "bearer",
			"expires_in":   604800,
		},
	})
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Email     string `json:"email"`
		Password  string `json:"password"`
		Role      string `json:"role"`
		FleetName string `json:"fleet_name"`
		Phone     string `json:"phone"`
		Image     string `json:"image"`
	}
	if err := readJSON(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Invalid JSON"})
		return
	}
	email := strings.TrimSpace(strings.ToLower(body.Email))
	if email == "" || body.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"success": false, "message": "Email and password required"})
		return
	}
	if _, ok := s.store.UserByEmail(email); ok {
		writeJSON(w, http.StatusConflict, map[string]any{"success": false, "message": "Email already registered"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "Could not hash password"})
		return
	}
	role := body.Role
	if role == "" {
		role = "fleet_owner"
	}
	name := body.FirstName
	lastName := body.LastName
	if body.FleetName != "" {
		name = body.FleetName
		lastName = ""
	}
	u := store.User{
		Email: email, PasswordHash: string(hash),
		Name: name, LastName: lastName, Phone: body.Phone,
		FleetImage: body.Image, Role: role, Status: 1,
	}
	if err := s.store.AppendUser(u); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": err.Error()})
		return
	}
	created, _ := s.store.UserByEmail(email)
	tok, err := s.store.NewSession(created.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "message": "Could not create session"})
		return
	}
	s.recordAudit(created.ID, "create", "user", created.ID, "Created user", map[string]any{"role": created.Role, "email": created.Email})
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"data": map[string]any{
			"user":         userPublic(created),
			"access_token": tok,
			"token_type":   "bearer",
			"expires_in":   604800,
		},
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	uid, _ := s.authUserID(r)
	if tok := bearerToken(r); tok != "" {
		s.store.DeleteSession(tok)
	}
	if uid != 0 {
		s.recordAudit(uid, "logout", "session", uid, "Logged out", nil)
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Logged out"})
}

func (s *Server) handleForgotPassword(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "If the account exists, a reset link was sent."})
}

func (s *Server) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "Password updated"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.requireAuth(w, r)
	if !ok {
		return
	}
	u, ok := s.store.UserByID(uid)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"success": false, "message": "User not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": userPublic(u)})
}
