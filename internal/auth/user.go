package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/albert-tarly/dungerra-api/internal/config"
	"github.com/albert-tarly/dungerra-api/internal/db"
)

// UserResponse is the public representation of an account.
type UserResponse struct {
	*db.User
	DisplayName string `json:"display_name"`
}

func newUserResponse(u *db.User) UserResponse {
	return UserResponse{User: u, DisplayName: u.DisplayName()}
}

// TokenResponse is returned by the credential-based login endpoints.
type TokenResponse struct {
	Token string       `json:"token"`
	User  UserResponse `json:"user"`
}

// ProvidersResponse tells clients which sign-in options to offer.
type ProvidersResponse struct {
	Google bool `json:"google"`
	Local  bool `json:"local"`
}

// UserHandler serves provider-independent account endpoints.
type UserHandler struct {
	userRepo db.UserRepository
}

func NewUserHandler() *UserHandler {
	return &UserHandler{userRepo: db.NewUserRepository()}
}

// HandleProviders returns the enabled authentication providers.
func (h *UserHandler) HandleProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ProvidersResponse{
		Google: config.AppConfig.GoogleAuthEnabled,
		Local:  config.AppConfig.LocalAuthEnabled,
	})
}

// HandleGetUser returns the current authenticated user's information.
func (h *UserHandler) HandleGetUser(w http.ResponseWriter, r *http.Request) {
	token := bearerToken(r)
	if token == "" {
		http.Error(w, "Missing authorization header", http.StatusUnauthorized)
		return
	}

	userID, err := ValidateToken(token)
	if err != nil {
		http.Error(w, "Invalid token", http.StatusUnauthorized)
		return
	}

	user, err := h.userRepo.FindByID(context.Background(), userID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			http.Error(w, "User not found", http.StatusNotFound)
		} else {
			http.Error(w, "Database error", http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusOK, newUserResponse(user))
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
