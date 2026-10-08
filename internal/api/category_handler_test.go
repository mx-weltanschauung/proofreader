package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

func TestNewCategoryHandler(t *testing.T) {
	handler := NewCategoryHandler(nil)
	if handler == nil {
		t.Fatal("Expected handler to be created")
	}
}

func TestCategoryHandler_Get_InvalidID(t *testing.T) {
	handler := NewCategoryHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/categories/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	handler.Get(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCategoryHandler_Create_InvalidJSON(t *testing.T) {
	handler := NewCategoryHandler(nil)

	req := httptest.NewRequest(http.MethodPost, "/api/categories", bytes.NewBufferString("invalid json"))
	rec := httptest.NewRecorder()

	handler.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCategoryHandler_Update_InvalidID(t *testing.T) {
	handler := NewCategoryHandler(nil)

	req := httptest.NewRequest(http.MethodPut, "/api/categories/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	handler.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCategoryHandler_Update_InvalidJSON(t *testing.T) {
	handler := NewCategoryHandler(nil)

	req := httptest.NewRequest(http.MethodPut, "/api/categories/1", bytes.NewBufferString("invalid json"))
	req = mux.SetURLVars(req, map[string]string{"id": "1"})
	rec := httptest.NewRecorder()

	handler.Update(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCategoryHandler_Delete_InvalidID(t *testing.T) {
	handler := NewCategoryHandler(nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/categories/invalid", nil)
	req = mux.SetURLVars(req, map[string]string{"id": "invalid"})
	rec := httptest.NewRecorder()

	handler.Delete(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateCategoryRequest(t *testing.T) {
	req := CreateCategoryRequest{
		Name:        "Test Category",
		Slug:        "test-category",
		Description: "Test description",
	}

	if req.Name != "Test Category" {
		t.Errorf("Name = %s, want Test Category", req.Name)
	}
	if req.Slug != "test-category" {
		t.Errorf("Slug = %s, want test-category", req.Slug)
	}
	if req.Description != "Test description" {
		t.Errorf("Description = %s, want Test description", req.Description)
	}
}

func TestCreateCategoryRequest_JSON(t *testing.T) {
	jsonStr := `{"name":"Fiction","slug":"fiction","description":"Fiction books"}`

	var req CreateCategoryRequest
	if err := json.Unmarshal([]byte(jsonStr), &req); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if req.Name != "Fiction" {
		t.Errorf("Name = %s, want Fiction", req.Name)
	}
	if req.Slug != "fiction" {
		t.Errorf("Slug = %s, want fiction", req.Slug)
	}
}
