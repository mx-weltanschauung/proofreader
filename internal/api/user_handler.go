package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/models"
)

// userStore is the subset of repository.UserRepository that UserHandler
// depends on. *repository.UserRepository satisfies this interface
// structurally, so the real repo can be passed in with no changes; tests
// can supply an in-memory fake instead.
type userStore interface {
	// ListStaff отдаёт сотрудников. Читатели в это окно не попадают: их
	// заводят и удаляют они сами — и видит их администратор отдельным
	// списком (ReaderAdminHandler), а не через управление сотрудниками.
	ListStaff(ctx context.Context, limit, offset int) ([]*models.User, error)
	Create(ctx context.Context, user *models.User) error
	GetByID(ctx context.Context, id int64) (*models.User, error)
	Update(ctx context.Context, user *models.User) error
	UpdatePassword(ctx context.Context, userID int64, passwordHash string) error
	Delete(ctx context.Context, id int64) error
	CountByRole(ctx context.Context, role models.UserRole) (int, error)
}

// UserHandler handles admin user-management endpoints.
type UserHandler struct {
	userRepo    userStore
	authService *auth.Service
}

// NewUserHandler creates a new user handler.
func NewUserHandler(userRepo userStore, authService *auth.Service) *UserHandler {
	return &UserHandler{userRepo: userRepo, authService: authService}
}

// CreateUserRequest is the request body for creating a user.
type CreateUserRequest struct {
	Email    string          `json:"email"`
	Password string          `json:"password"`
	Role     models.UserRole `json:"role"`
}

// UpdateUserRequest is the request body for updating a user.
type UpdateUserRequest struct {
	Role     *models.UserRole `json:"role,omitempty"`
	Password *string          `json:"password,omitempty"`
}

func validRole(role models.UserRole) bool {
	return role == models.RoleAdministrator || role == models.RoleEditor
}

// List returns staff users. Читатели сюда не попадают — см. ListStaff.
func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	users, err := h.userRepo.ListStaff(r.Context(), 500, 0)
	if err != nil {
		http.Error(w, "failed to list users", http.StatusInternalServerError)
		return
	}
	out := make([]UserResponse, 0, len(users))
	for _, u := range users {
		out = append(out, UserResponse{ID: u.ID, Email: u.Email, Role: u.Role})
	}
	writeJSON(w, out)
}

// Create creates a new user.
func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Email == "" || req.Password == "" || !validRole(req.Role) {
		http.Error(w, "email, password, and a valid role are required", http.StatusBadRequest)
		return
	}
	hash, err := h.authService.HashPassword(req.Password)
	if err != nil {
		http.Error(w, "failed to hash password", http.StatusInternalServerError)
		return
	}
	u := &models.User{Email: req.Email, PasswordHash: hash, Role: req.Role}
	if err := h.userRepo.Create(r.Context(), u); err != nil {
		http.Error(w, "failed to create user", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	writeJSON(w, UserResponse{ID: u.ID, Email: u.Email, Role: u.Role})
}

// Update updates a user's role and/or password, guarding against demoting
// the last administrator.
func (h *UserHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	u, err := h.userRepo.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if req.Role != nil {
		if !validRole(*req.Role) {
			http.Error(w, "invalid role", http.StatusBadRequest)
			return
		}
		// Guard: don't demote the last administrator.
		if u.Role == models.RoleAdministrator && *req.Role != models.RoleAdministrator {
			n, err := h.userRepo.CountByRole(r.Context(), models.RoleAdministrator)
			if err != nil {
				http.Error(w, "failed to check admins", http.StatusInternalServerError)
				return
			}
			if n <= 1 {
				http.Error(w, "cannot demote the last administrator", http.StatusConflict)
				return
			}
		}
		u.Role = *req.Role
		if err := h.userRepo.Update(r.Context(), u); err != nil {
			http.Error(w, "failed to update user", http.StatusInternalServerError)
			return
		}
	}
	if req.Password != nil && *req.Password != "" {
		hash, err := h.authService.HashPassword(*req.Password)
		if err != nil {
			http.Error(w, "failed to hash password", http.StatusInternalServerError)
			return
		}
		if err := h.userRepo.UpdatePassword(r.Context(), id, hash); err != nil {
			http.Error(w, "failed to update password", http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, UserResponse{ID: u.ID, Email: u.Email, Role: u.Role})
}

// Delete deletes a user, guarding against deleting the last administrator.
func (h *UserHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(mux.Vars(r)["id"], 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	u, err := h.userRepo.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if u.Role == models.RoleAdministrator {
		n, err := h.userRepo.CountByRole(r.Context(), models.RoleAdministrator)
		if err != nil {
			http.Error(w, "failed to check admins", http.StatusInternalServerError)
			return
		}
		if n <= 1 {
			http.Error(w, "cannot delete the last administrator", http.StatusConflict)
			return
		}
	}
	if err := h.userRepo.Delete(r.Context(), id); err != nil {
		http.Error(w, "failed to delete user", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
