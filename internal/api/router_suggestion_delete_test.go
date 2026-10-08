package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

// Литерал /suggestions/rejected обязан стоять в router.go ВЫШЕ шаблона
// /suggestions/{id}: оба маршрута живут в одном подроутере staff, и порядок
// строк — единственное, что их разводит (в отличие от пары mine/{id}, где
// разводит порядок создания подроутеров). Переставь их — и массовая чистка
// уедет в удаление предложения с идентификатором "rejected", то есть в 400.
func TestPurgeRouteBeatsSuggestionIDRoute(t *testing.T) {
	muxRouter := createTestRouter().Setup()

	req := httptest.NewRequest(http.MethodDelete, "/api/suggestions/rejected", nil)
	var match mux.RouteMatch
	if !muxRouter.Match(req, &match) {
		t.Fatal("DELETE /api/suggestions/rejected не совпал ни с одним маршрутом")
	}
	tmpl, err := match.Route.GetPathTemplate()
	if err != nil {
		t.Fatalf("шаблон маршрута: %v", err)
	}
	if tmpl != "/api/suggestions/rejected" {
		t.Fatalf("запрос ушёл в %q вместо /api/suggestions/rejected", tmpl)
	}
}
