package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"copilot-local-api/internal/store"
)

func TestLoginUsesStoredSQLiteUsers(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "database.sqlite"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer st.Close()

	hash, err := bcrypt.GenerateFromPassword([]byte("stored-secret"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if err := st.AppendUser(store.User{
		Email:        "stored@example.com",
		PasswordHash: string(hash),
		Name:         "Stored",
		LastName:     "User",
		Role:         "installation",
		Status:       1,
	}); err != nil {
		t.Fatalf("append user: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{
		"email": " stored@example.com ",
		"password": "stored-secret"
	}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	New(st).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			User struct {
				Email string `json:"email"`
				Role  string `json:"role"`
			} `json:"user"`
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Success {
		t.Fatal("expected success response")
	}
	if response.Data.User.Email != "stored@example.com" {
		t.Fatalf("email = %q", response.Data.User.Email)
	}
	if response.Data.User.Role != "installation" {
		t.Fatalf("role = %q", response.Data.User.Role)
	}
	if response.Data.AccessToken == "" {
		t.Fatal("expected access token")
	}
}
