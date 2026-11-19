package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/besuhoff/dungerra/internal/db"
	"golang.org/x/crypto/bcrypt"
)

const (
	minUsernameLen = 3
	maxUsernameLen = 24
	minPasswordLen = 8
	maxPasswordLen = 72 // bcrypt ignores bytes beyond 72
	maxAuthBody    = 4 << 10
)

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

var (
	errInvalidCredentials = errors.New("invalid username or password")
	errUsernameTaken      = errors.New("username is already taken")
)

// LocalCredentials is the request body for local register/login.
type LocalCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LocalAuthHandler provides username + password accounts that live entirely
// in this server's database (no third-party identity provider).
type LocalAuthHandler struct {
	userRepo db.UserRepository
}

func NewLocalAuthHandler() *LocalAuthHandler {
	return &LocalAuthHandler{userRepo: db.NewUserRepository()}
}

// HandleRegister creates a local account and signs it in.
// POST {"username", "password"} → 201 TokenResponse | 400 | 409
func (h *LocalAuthHandler) HandleRegister(w http.ResponseWriter, r *http.Request) {
	creds, ok := decodeCredentials(w, r)
	if !ok {
		return
	}
	if err := validateUsername(creds.Username); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := validatePassword(creds.Password); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	user, err := h.register(r.Context(), creds)
	switch {
	case errors.Is(err, errUsernameTaken):
		http.Error(w, err.Error(), http.StatusConflict)
		return
	case err != nil:
		log.Printf("Local register failed: %v", err)
		http.Error(w, "Failed to create user", http.StatusInternalServerError)
		return
	}

	respondWithToken(w, http.StatusCreated, user)
}

// HandleLogin signs in an existing local account.
// POST {"username", "password"} → 200 TokenResponse | 401
func (h *LocalAuthHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	creds, ok := decodeCredentials(w, r)
	if !ok {
		return
	}

	user, err := h.login(r.Context(), creds)
	switch {
	case errors.Is(err, errInvalidCredentials):
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	case err != nil:
		log.Printf("Local login failed: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	respondWithToken(w, http.StatusOK, user)
}

func (h *LocalAuthHandler) register(ctx context.Context, creds LocalCredentials) (*db.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(creds.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	user := &db.User{
		Provider:     db.ProviderLocal,
		ProviderID:   localProviderID(creds.Username),
		Username:     creds.Username,
		PasswordHash: string(hash),
	}
	// The (provider, provider_id) unique constraint is the source of truth,
	// so concurrent registrations of the same name can't both succeed.
	if err := h.userRepo.Create(ctx, user); err != nil {
		if errors.Is(err, db.ErrDuplicate) {
			return nil, errUsernameTaken
		}
		return nil, err
	}
	return user, nil
}

func (h *LocalAuthHandler) login(ctx context.Context, creds LocalCredentials) (*db.User, error) {
	user, err := h.userRepo.FindByProvider(ctx, db.ProviderLocal, localProviderID(creds.Username))
	if errors.Is(err, db.ErrNotFound) {
		// Spend the same time as a real check so response timing doesn't
		// reveal which usernames exist.
		bcrypt.CompareHashAndPassword(dummyHash(), []byte(creds.Password))
		return nil, errInvalidCredentials
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(creds.Password)) != nil || !user.IsActive {
		return nil, errInvalidCredentials
	}
	return user, nil
}

// localProviderID makes local usernames case-insensitively unique.
func localProviderID(username string) string {
	return strings.ToLower(username)
}

func validateUsername(username string) error {
	if len(username) < minUsernameLen || len(username) > maxUsernameLen {
		return fmt.Errorf("username must be %d-%d characters", minUsernameLen, maxUsernameLen)
	}
	if !usernamePattern.MatchString(username) {
		return errors.New("username may only contain letters, digits, '_', '-' and '.'")
	}
	return nil
}

func validatePassword(password string) error {
	if len(password) < minPasswordLen || len(password) > maxPasswordLen {
		return fmt.Errorf("password must be %d-%d characters", minPasswordLen, maxPasswordLen)
	}
	return nil
}

func decodeCredentials(w http.ResponseWriter, r *http.Request) (LocalCredentials, bool) {
	var creds LocalCredentials
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return creds, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBody)
	if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return creds, false
	}
	creds.Username = strings.TrimSpace(creds.Username)
	return creds, true
}

func respondWithToken(w http.ResponseWriter, status int, user *db.User) {
	token, err := GenerateToken(user.ID)
	if err != nil {
		http.Error(w, "Failed to generate token", http.StatusInternalServerError)
		return
	}
	writeJSON(w, status, TokenResponse{Token: token, User: newUserResponse(user)})
}

var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("dummy-password-for-timing"), bcrypt.DefaultCost)
	return h
})
