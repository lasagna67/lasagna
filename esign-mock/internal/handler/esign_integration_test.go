package handler_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"esign-mock/internal/handler"
	"esign-mock/internal/repository"
	"esign-mock/internal/service"
)

const certificatesFixture = `[
  {"serial":"A1","type":"legal_entity_head",
   "subject":{"full_name":"Ромашкина Ирина Петровна","inn":"770112345678","position":"Генеральный директор"},
   "organization":{"name":"ООО «Ромашка»","inn":"7701234567"},
   "issuer":"УЦ ФНС России (демо)","valid_from":"2026-01-01","valid_to":"2099-01-01","status":"valid","pin":"12345678"},
  {"serial":"B2","type":"individual_entrepreneur",
   "subject":{"full_name":"Петров Иван Сергеевич","inn":"770123456789"},
   "organization":null,
   "issuer":"УЦ ФНС России (демо)","valid_from":"2026-01-01","valid_to":"2099-01-01","status":"revoked","pin":"12345678"}
]`

func newRoutes(t *testing.T, origins ...string) http.Handler {
	t.Helper()
	path := filepath.Join(t.TempDir(), "certificates.json")
	if err := os.WriteFile(path, []byte(certificatesFixture), 0600); err != nil {
		t.Fatal(err)
	}
	r, err := repository.NewJSONRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(origins) == 0 {
		origins = []string{"*"}
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	return handler.NewESignHandler(service.NewESignService(r, "test-secret"), logger, origins).Routes()
}

func do(t *testing.T, routes http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
	return w
}

func decode(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return v
}

func TestIntegrationSignVerifyRoundTrip(t *testing.T) {
	routes := newRoutes(t)
	const data = "Подключение компании ИНН 7701234567. Код 42"

	signed := do(t, routes, http.MethodPost, "/v1/sign", `{"certificate_serial":"A1","pin":"12345678","data":"`+data+`"}`)
	if signed.Code != http.StatusOK {
		t.Fatalf("sign: %d %s", signed.Code, signed.Body)
	}
	signature := decode(t, signed.Body.Bytes())["signature"].(string)

	body, _ := json.Marshal(map[string]string{"data": data, "signature": signature})
	verified := do(t, routes, http.MethodPost, "/v1/verify", string(body))
	got := decode(t, verified.Body.Bytes())
	cert, _ := got["certificate"].(map[string]any)
	org, _ := cert["organization"].(map[string]any)
	if verified.Code != http.StatusOK || got["valid"] != true || org["inn"] != "7701234567" {
		t.Fatalf("verify: %d %s", verified.Code, verified.Body)
	}

	body, _ = json.Marshal(map[string]string{"data": data + "!", "signature": signature})
	got = decode(t, do(t, routes, http.MethodPost, "/v1/verify", string(body)).Body.Bytes())
	if got["valid"] != false || got["reason"] != "signature_mismatch" {
		t.Fatalf("tampered data verified: %v", got)
	}
}

func TestIntegrationErrors(t *testing.T) {
	routes := newRoutes(t)
	for _, tt := range []struct {
		name, method, path, body string
		status                   int
		code                     string
	}{
		{"wrong PIN", http.MethodPost, "/v1/sign", `{"certificate_serial":"A1","pin":"0000","data":"x"}`, http.StatusForbidden, "INVALID_PIN"},
		{"revoked", http.MethodPost, "/v1/sign", `{"certificate_serial":"B2","pin":"12345678","data":"x"}`, http.StatusUnprocessableEntity, "CERTIFICATE_REVOKED"},
		{"unknown certificate", http.MethodPost, "/v1/sign", `{"certificate_serial":"ZZ","pin":"12345678","data":"x"}`, http.StatusNotFound, "CERTIFICATE_NOT_FOUND"},
		{"missing fields", http.MethodPost, "/v1/sign", `{"certificate_serial":"A1"}`, http.StatusBadRequest, "INVALID_REQUEST"},
		{"bad JSON", http.MethodPost, "/v1/sign", `{`, http.StatusBadRequest, "INVALID_REQUEST"},
		{"malformed signature", http.MethodPost, "/v1/verify", `{"data":"x","signature":"%%%"}`, http.StatusBadRequest, "MALFORMED_SIGNATURE"},
		{"wrong method", http.MethodGet, "/v1/sign", ``, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED"},
		{"unknown route", http.MethodGet, "/v1/nope", ``, http.StatusNotFound, "NOT_FOUND"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			w := do(t, routes, tt.method, tt.path, tt.body)
			got, _ := decode(t, w.Body.Bytes())["error"].(map[string]any)
			if w.Code != tt.status || got["code"] != tt.code {
				t.Fatalf("got %d %s, want %d %s", w.Code, w.Body, tt.status, tt.code)
			}
		})
	}
}

func TestIntegrationCertificatesHidePIN(t *testing.T) {
	w := do(t, newRoutes(t), http.MethodGet, "/v1/certificates", "")
	if w.Code != http.StatusOK || bytes.Contains(w.Body.Bytes(), []byte(`"pin"`)) {
		t.Fatalf("certificates leak PIN or failed: %d %s", w.Code, w.Body)
	}
	if list := decode(t, w.Body.Bytes())["certificates"].([]any); len(list) != 2 {
		t.Fatalf("want 2 certificates, got %d", len(list))
	}
}

func TestIntegrationCORS(t *testing.T) {
	routes := newRoutes(t, "http://localhost:3000")

	req := httptest.NewRequest(http.MethodOptions, "/v1/sign", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()
	routes.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatalf("preflight: %d %v", w.Code, w.Header())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/certificates", nil)
	req.Header.Set("Origin", "http://evil.example")
	w = httptest.NewRecorder()
	routes.ServeHTTP(w, req)
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("foreign origin allowed: %v", w.Header())
	}
}
