package handler_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"registry-mock/internal/handler"
	"registry-mock/internal/repository"
	"registry-mock/internal/service"
)

const fixture = `[
  {
    "inn":"7705005410","ogrn":"1127706709294","kpp":"770501001",
    "entity_type":"ЮЛ","source":"ЕГРЮЛ",
    "name":{"full":"Демо компания","short":"Демо"},
    "registration":{"date":"2012-04-17","region":"Москва","address":"Демо адрес"},
    "status":{"code":"reorganizing","description":"Реорганизация","since":"2026-01-01"},
    "capital":{"amount_kopecks":50000000,"type":"authorized"},
    "activities":{"primary_okved":"25.62","additional_okveds":["25.61","46.72"]},
    "reorganization":{"type":"merger","start_date":"2026-01-01","description":"Слияние"},
    "metadata":{"as_of":"2026-10-20","is_synthetic":true,"demo_scenario":"private-marker"}
  },
  {
    "inn":"012345678901","ogrn":"012345678901234","entity_type":"ИП","source":"ЕГРИП",
    "name":{"full":"ИП Демо","short":"ИП Демо"},
    "registration":{"date":"2020-01-01","region":"Москва","address":null},
    "status":{"code":"active","description":"Действующий ИП","since":"2020-01-01"},
    "activities":{"primary_okved":"62.01","additional_okveds":[]},
    "metadata":{"as_of":"2026-10-20","is_synthetic":true,"demo_scenario":"private-marker"}
  }
]`

func newRoutes(t *testing.T) http.Handler {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := repository.NewJSONRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	return handler.NewRegistryHandler(service.NewRegistryService(r)).Routes()
}

func TestIntegrationCompanyPublicContract(t *testing.T) {
	want := `{
      "inn":"7705005410","ogrn":"1127706709294","kpp":"770501001",
      "entity_type":"ЮЛ","source":"ЕГРЮЛ",
      "name":{"full":"Демо компания","short":"Демо"},
      "registration":{"date":"2012-04-17","region":"Москва","address":"Демо адрес"},
      "status":{"code":"reorganizing","description":"Реорганизация","since":"2026-01-01"},
      "capital":{"amount_kopecks":50000000,"type":"authorized"},
      "activities":{"primary_okved":"25.62","additional_okveds":["25.61","46.72"]},
      "reorganization":{"type":"merger","start_date":"2026-01-01","description":"Слияние"}
    }`
	w := httptest.NewRecorder()
	newRoutes(t).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/companies/7705005410", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected content type: %q", w.Header().Get("Content-Type"))
	}
	if got := decodeObject(t, w.Body.Bytes()); !reflect.DeepEqual(got, decodeObject(t, []byte(want))) {
		t.Fatalf("public company differs from contract: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "metadata") || strings.Contains(w.Body.String(), "private-marker") {
		t.Fatalf("private metadata leaked: %s", w.Body.String())
	}
}

func TestIntegrationIndividualEntrepreneurOptionalFields(t *testing.T) {
	w := httptest.NewRecorder()
	newRoutes(t).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/companies/012345678901", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	got := decodeObject(t, w.Body.Bytes())
	if got["inn"] != "012345678901" || got["ogrn"] != "012345678901234" || got["source"] != "ЕГРИП" || got["entity_type"] != "ИП" {
		t.Fatalf("Individual Entrepreneur identifiers or type changed: %v", got)
	}
	for _, field := range []string{"kpp", "capital", "reorganization", "metadata"} {
		if _, ok := got[field]; ok {
			t.Errorf("unexpected field %q: %v", field, got[field])
		}
	}
	registration, ok := got["registration"].(map[string]any)
	if !ok {
		t.Fatal("missing registration object")
	}
	if address, exists := registration["address"]; !exists || address != nil {
		t.Fatalf("want explicit null address, got %v", registration)
	}
	activities, ok := got["activities"].(map[string]any)
	if !ok || !reflect.DeepEqual(activities["additional_okveds"], []any{}) {
		t.Fatalf("want empty additional_okveds array, got %v", got["activities"])
	}
}

func TestIntegrationRoutes(t *testing.T) {
	routes := newRoutes(t)
	tests := []struct {
		name, method, path string
		status             int
		code               string
	}{
		{"health", "GET", "/health", 200, ""},
		{"short INN", "GET", "/v1/companies/123", 400, "INVALID_INN"},
		{"letters in INN", "GET", "/v1/companies/770500541x", 400, "INVALID_INN"},
		{"missing company", "GET", "/v1/companies/9999999999", 404, "COMPANY_NOT_FOUND"},
		{"wrong method", "POST", "/v1/companies/7705005410", 405, "METHOD_NOT_ALLOWED"},
		{"health wrong method", "POST", "/health", 405, "METHOD_NOT_ALLOWED"},
		{"unknown path", "GET", "/unknown", 404, "NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			routes.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))
			if w.Code != tt.status || w.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected response: %d, %v, %s", w.Code, w.Header(), w.Body.String())
			}
			got := decodeObject(t, w.Body.Bytes())
			if tt.code == "" {
				if !reflect.DeepEqual(got, map[string]any{"status": "ok"}) {
					t.Fatalf("unexpected health response: %v", got)
				}
				return
			}
			errBody, ok := got["error"].(map[string]any)
			if !ok || errBody["code"] != tt.code || errBody["message"] == "" || len(errBody) != 2 || len(got) != 1 {
				t.Fatalf("unexpected error envelope: %v", got)
			}
			if tt.status == http.StatusMethodNotAllowed && w.Header().Get("Allow") != "GET" {
				t.Fatalf("missing Allow: GET header: %v", w.Header())
			}
		})
	}
}
