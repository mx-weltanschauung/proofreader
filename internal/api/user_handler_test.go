package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"proofreader/internal/auth"
	"proofreader/internal/config"
	"proofreader/internal/models"
)

// fakeUserStore is an in-memory implementation of userStore for DB-free
// handler tests.
type fakeUserStore struct {
	users  map[int64]*models.User
	nextID int64
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{users: make(map[int64]*models.User)}
}

func (f *fakeUserStore) ListStaff(ctx context.Context, limit, offset int) ([]*models.User, error) {
	out := make([]*models.User, 0, len(f.users))
	for _, u := range f.users {
		if u.Role == models.RoleReader {
			continue
		}
		out = append(out, u)
	}
	return out, nil
}

func (f *fakeUserStore) Create(ctx context.Context, user *models.User) error {
	f.nextID++
	user.ID = f.nextID
	user.CreatedAt = time.Now()
	user.UpdatedAt = time.Now()
	cp := *user
	f.users[user.ID] = &cp
	return nil
}

func (f *fakeUserStore) GetByID(ctx context.Context, id int64) (*models.User, error) {
	u, ok := f.users[id]
	if !ok {
		return nil, errors.New("user not found")
	}
	cp := *u
	return &cp, nil
}

func (f *fakeUserStore) Update(ctx context.Context, user *models.User) error {
	existing, ok := f.users[user.ID]
	if !ok {
		return errors.New("user not found")
	}
	existing.Email = user.Email
	existing.Role = user.Role
	existing.UpdatedAt = time.Now()
	return nil
}

func (f *fakeUserStore) UpdatePassword(ctx context.Context, userID int64, passwordHash string) error {
	u, ok := f.users[userID]
	if !ok {
		return errors.New("user not found")
	}
	u.PasswordHash = passwordHash
	return nil
}

func (f *fakeUserStore) Delete(ctx context.Context, id int64) error {
	if _, ok := f.users[id]; !ok {
		return errors.New("user not found")
	}
	delete(f.users, id)
	return nil
}

func (f *fakeUserStore) CountByRole(ctx context.Context, role models.UserRole) (int, error) {
	n := 0
	for _, u := range f.users {
		if u.Role == role {
			n++
		}
	}
	return n, nil
}

func newTestUserHandler(t *testing.T) (*UserHandler, *fakeUserStore) {
	t.Helper()
	store := newFakeUserStore()
	authService := auth.NewService(&config.JWTConfig{
		Secret:            "test-secret",
		Expiration:        time.Hour,
		RefreshExpiration: 24 * time.Hour,
	})
	return NewUserHandler(store, authService), store
}

func mustCreate(t *testing.T, store *fakeUserStore, email string, role models.UserRole) *models.User {
	t.Helper()
	u := &models.User{Email: email, PasswordHash: "hash", Role: role}
	if err := store.Create(context.Background(), u); err != nil {
		t.Fatalf("mustCreate: %v", err)
	}
	return u
}

func TestUpdateUser_CannotDemoteLastAdmin(t *testing.T) {
	h, store := newTestUserHandler(t)
	admin := mustCreate(t, store, "admin@x.io", models.RoleAdministrator)
	// only one admin exists

	body := `{"role":"editor"}`
	req := httptest.NewRequest("PUT", "/api/users/"+strconv.FormatInt(admin.ID, 10), strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"id": strconv.FormatInt(admin.ID, 10)})
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("demote last admin = %d, want 409", rec.Code)
	}
}

func TestDeleteUser_CannotDeleteLastAdmin(t *testing.T) {
	h, store := newTestUserHandler(t)
	admin := mustCreate(t, store, "admin@x.io", models.RoleAdministrator)

	req := httptest.NewRequest("DELETE", "/api/users/"+strconv.FormatInt(admin.ID, 10), nil)
	req = mux.SetURLVars(req, map[string]string{"id": strconv.FormatInt(admin.ID, 10)})
	rec := httptest.NewRecorder()

	h.Delete(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("delete last admin = %d, want 409", rec.Code)
	}
	if _, err := store.GetByID(context.Background(), admin.ID); err != nil {
		t.Fatalf("admin should still exist after blocked delete, got err: %v", err)
	}
}

func TestUpdateUser_CanDemoteAdminWhenTwoAdminsExist(t *testing.T) {
	h, store := newTestUserHandler(t)
	admin1 := mustCreate(t, store, "admin1@x.io", models.RoleAdministrator)
	mustCreate(t, store, "admin2@x.io", models.RoleAdministrator)

	body := `{"role":"editor"}`
	req := httptest.NewRequest("PUT", "/api/users/"+strconv.FormatInt(admin1.ID, 10), strings.NewReader(body))
	req = mux.SetURLVars(req, map[string]string{"id": strconv.FormatInt(admin1.ID, 10)})
	rec := httptest.NewRecorder()

	h.Update(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("demote admin with two admins = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp UserResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Role != models.RoleEditor {
		t.Fatalf("resp.Role = %q, want editor", resp.Role)
	}
}

func TestListUsersExcludesReaders(t *testing.T) {
	h, store := newTestUserHandler(t)
	mustCreate(t, store, "admin@x.io", models.RoleAdministrator)
	mustCreate(t, store, "reader@x.io", models.RoleReader)

	req := httptest.NewRequest("GET", "/api/users", nil)
	rec := httptest.NewRecorder()

	h.List(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("list users = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp []UserResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, u := range resp {
		if u.Role == models.RoleReader {
			t.Fatalf("resp contains a reader account: %+v", u)
		}
	}
	if len(resp) != 1 {
		t.Fatalf("len(resp) = %d, want 1 (admin only)", len(resp))
	}
}

func TestCreateUser_RejectsBadRole(t *testing.T) {
	h, _ := newTestUserHandler(t)
	req := httptest.NewRequest("POST", "/api/users", strings.NewReader(`{"email":"e@x.io","password":"pw","role":"proofreader"}`))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad role = %d, want 400", rec.Code)
	}
}

func TestCreateUser_ValidEditor(t *testing.T) {
	h, store := newTestUserHandler(t)
	req := httptest.NewRequest("POST", "/api/users", strings.NewReader(`{"email":"editor@x.io","password":"pw","role":"editor"}`))
	rec := httptest.NewRecorder()

	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("create valid editor = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp UserResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Email != "editor@x.io" || resp.Role != models.RoleEditor {
		t.Fatalf("resp = %+v, want editor@x.io/editor", resp)
	}
	if _, err := store.GetByID(context.Background(), resp.ID); err != nil {
		t.Fatalf("created user not found in store: %v", err)
	}
}
