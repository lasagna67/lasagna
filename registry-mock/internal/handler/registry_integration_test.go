package handler_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"registry-mock/internal/handler"
	"registry-mock/internal/repository"
	"registry-mock/internal/service"
)

const egripFixture = `[
  {"inn":"770123456789","status":"ok","report":{
    "business_type":"ИП","name":"ИП Петров Иван Сергеевич",
    "registration_date":"2019-04-15","business_status":"active",
    "okved":[{"code":"47.91","name":"Торговля розничная по почте или по информационно-коммуникационной сети Интернет"}]
  }},
  {"inn":"770987654321","status":"ok","report":{
    "business_type":"ИП","name":"ИП Сидоров Алексей Петрович",
    "registration_date":"2016-02-10","business_status":"terminated","termination_date":"2025-11-30",
    "okved":[{"code":"56.10","name":"Деятельность ресторанов и услуги по доставке продуктов питания"}]
  }}
]`

const egrulFixture = `[
  {"inn":"7701234567","status":"ok","report":{
    "business_type":"ООО","name":"ООО «Ромашка»",
    "registration_date":"2018-06-20","business_status":"active",
    "head":{"full_name":"Ромашкина Ирина Петровна","position":"Генеральный директор","inn":"770112345678"},
    "okved":[{"code":"46.90","name":"Торговля оптовая неспециализированная"}]
  }},
  {"inn":"7709876543","status":"ok","report":{
    "business_type":"ООО","name":"ООО «Демо»",
    "registration_date":"2016-02-10","business_status":"terminated","termination_date":"2025-11-30",
    "okved":[]
  }}
]`

func newRoutes(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	egripPath, egrulPath := filepath.Join(dir, "egrip.json"), filepath.Join(dir, "egrul.json")
	for path, data := range map[string]string{egripPath: egripFixture, egrulPath: egrulFixture} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	r, err := repository.NewJSONRepository(egripPath, egrulPath)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return handler.NewRegistryHandler(service.NewRegistryService(r), logger).Routes()
}

func TestIntegrationPublicReportContract(t *testing.T) {
	routes := newRoutes(t)
	for _, tt := range []struct {
		path, want string
	}{
		{"/v1/egrip/770123456789", `{
          "inn":"770123456789","status":"ok","report":{
            "business_type":"ИП","name":"ИП Петров Иван Сергеевич",
            "registration_date":"2019-04-15","business_status":"active",
            "okved":[{"code":"47.91","name":"Торговля розничная по почте или по информационно-коммуникационной сети Интернет"}]
          }
        }`},
		{"/v1/egrul/7701234567", `{
          "inn":"7701234567","status":"ok","report":{
            "business_type":"ООО","name":"ООО «Ромашка»",
            "registration_date":"2018-06-20","business_status":"active",
            "head":{"full_name":"Ромашкина Ирина Петровна","position":"Генеральный директор","inn":"770112345678"},
            "okved":[{"code":"46.90","name":"Торговля оптовая неспециализированная"}]
          }
        }`},
	} {
		t.Run(tt.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			routes.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
			}
			if !reflect.DeepEqual(decodeObject(t, w.Body.Bytes()), decodeObject(t, []byte(tt.want))) {
				t.Fatalf("report differs from public contract: %s", w.Body.String())
			}
		})
	}
}

func TestIntegrationTerminatedBusinessReport(t *testing.T) {
	routes := newRoutes(t)
	for _, path := range []string{"/v1/egrip/770987654321", "/v1/egrul/7709876543"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			routes.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
			got := decodeObject(t, w.Body.Bytes())
			report, ok := got["report"].(map[string]any)
			if w.Code != 200 || got["status"] != "ok" || !ok || report["business_status"] != "terminated" || report["termination_date"] != "2025-11-30" {
				t.Fatalf("unexpected terminated business report: %d %v", w.Code, got)
			}
			if path == "/v1/egrul/7709876543" && !reflect.DeepEqual(report["okved"], []any{}) {
				t.Fatalf("empty OKVED list changed: %v", report["okved"])
			}
		})
	}
}

func TestIntegrationRoutes(t *testing.T) {
	routes := newRoutes(t)
	for _, tt := range []struct {
		name, method, path string
		status             int
		code               string
	}{
		{"health", "GET", "/health", 200, ""},
		{"short INN", "GET", "/v1/egrip/123", 400, "INVALID_INN"},
		{"EGRIP wrong INN type", "GET", "/v1/egrip/7701234567", 400, "INVALID_INN"},
		{"EGRUL wrong INN type", "GET", "/v1/egrul/770123456789", 400, "INVALID_INN"},
		{"letters in EGRUL INN", "GET", "/v1/egrul/770123456x", 400, "INVALID_INN"},
		{"letters in EGRIP INN", "GET", "/v1/egrip/77012345678x", 400, "INVALID_INN"},
		{"missing EGRUL company", "GET", "/v1/egrul/9999999999", 404, "COMPANY_NOT_FOUND"},
		{"missing EGRIP company", "GET", "/v1/egrip/999999999999", 404, "COMPANY_NOT_FOUND"},
		{"EGRUL wrong method", "POST", "/v1/egrul/7701234567", 405, "METHOD_NOT_ALLOWED"},
		{"EGRIP wrong method", "POST", "/v1/egrip/770123456789", 405, "METHOD_NOT_ALLOWED"},
		{"health wrong method", "POST", "/health", 405, "METHOD_NOT_ALLOWED"},
		{"old route", "GET", "/v1/companies/7701234567", 404, "NOT_FOUND"},
		{"unknown path", "GET", "/unknown", 404, "NOT_FOUND"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			routes.ServeHTTP(w, httptest.NewRequest(tt.method, tt.path, nil))
			if w.Code != tt.status || w.Header().Get("Content-Type") != "application/json" {
				t.Fatalf("unexpected response: %d %v %s", w.Code, w.Header(), w.Body.String())
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
			if tt.status == 405 && w.Header().Get("Allow") != "GET" {
				t.Fatalf("missing Allow: GET header: %v", w.Header())
			}
		})
	}
}
