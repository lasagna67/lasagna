package companyhttpjson

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

// newTestClient поднимает фейковые registry-mock и esign-mock. Ответы — по пути запроса.
func newTestClient(t *testing.T, responses map[string]func(w http.ResponseWriter, r *http.Request)) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handle, ok := responses[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		handle(w, r)
	}))
	t.Cleanup(srv.Close)
	endpoint := Endpoint{URL: srv.URL + "/", Timeout: time.Second}
	return New(endpoint, endpoint)
}

func reply(status int, body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestGetCard(t *testing.T) {
	c := newTestClient(t, map[string]func(http.ResponseWriter, *http.Request){
		"/v1/egrul/7705005410": reply(200, `{"inn":"7705005410","status":"ok","report":{
			"business_type":"ООО","name":"ООО «Кедровый контур Демо»","registration_date":"2012-04-17",
			"business_status":"active",
			"head":{"full_name":"Кедрова Анна Викторовна","position":"Генеральный директор","inn":"770586906621"},
			"okved":[{"code":"25.62","name":"Обработка"},{"code":"46.72","name":"Торговля"}]}}`),
		"/v1/egrip/526200583660": reply(200, `{"inn":"526200583660","status":"ok","report":{
			"business_type":"ИП","name":"ИП Ручейников Глеб Павлович","registration_date":"2017-04-11",
			"business_status":"terminated","termination_date":"2025-07-18","okved":[]}}`),
		"/v1/egrul/1234567890": reply(404, `{"error":{"code":"COMPANY_NOT_FOUND","message":"Company not found"}}`),
		"/v1/egrul/1111111111": reply(500, `{"error":{"code":"INTERNAL_ERROR","message":"boom"}}`),
		"/v1/egrul/2222222222": reply(200, `{"inn":"2222222222","report":{"name":"x","registration_date":"2020-01-01","business_status":"sleeping"}}`),
	})
	ctx := context.Background()

	llc, err := c.GetCard(ctx, "7705005410")
	if err != nil {
		t.Fatalf("ООО: %v", err)
	}
	if llc.EntityType != companydomain.EntityLegal || llc.Head == nil || llc.Head.INN != "770586906621" ||
		len(llc.OKVED) != 2 || llc.RegistrationDate.Format(time.DateOnly) != "2012-04-17" {
		t.Errorf("unexpected ООО card: %+v", llc)
	}

	ip, err := c.GetCard(ctx, "526200583660")
	if err != nil {
		t.Fatalf("ИП: %v", err)
	}
	if ip.EntityType != companydomain.EntityIndividualEntrepreneur || ip.Status != companydomain.StatusTerminated ||
		ip.TerminationDate == nil || ip.Head != nil {
		t.Errorf("unexpected ИП card: %+v", ip)
	}

	for inn, want := range map[string]error{
		"1234567890": companyerrors.ErrNotInRegistry,
		"1111111111": companyerrors.ErrRegistryUnavailable,
		"2222222222": companyerrors.ErrRegistryUnavailable, // неизвестный статус
	} {
		if _, err := c.GetCard(ctx, inn); !errors.Is(err, want) {
			t.Errorf("GetCard(%s) err = %v, want %v", inn, err, want)
		}
	}
}

func TestGetCardRegistryDown(t *testing.T) {
	c := New(Endpoint{URL: "http://127.0.0.1:1", Timeout: time.Second}, Endpoint{})
	if _, err := c.GetCard(context.Background(), "7705005410"); !errors.Is(err, companyerrors.ErrRegistryUnavailable) {
		t.Fatalf("err = %v, want ErrRegistryUnavailable", err)
	}
}

func TestVerifySignature(t *testing.T) {
	var answer func(http.ResponseWriter, *http.Request)
	c := newTestClient(t, map[string]func(http.ResponseWriter, *http.Request){
		"/v1/verify": func(w http.ResponseWriter, r *http.Request) {
			var req struct{ Data, Signature string }
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil || r.Method != http.MethodPost || req.Data != "заявление" {
				t.Errorf("bad verify request: %s %+v %v", r.Method, req, err)
			}
			answer(w, r)
		},
	})
	verify := func() (companydomain.Certificate, error) {
		return c.VerifySignature(context.Background(), "заявление", "sig")
	}

	answer = reply(200, `{"valid":true,"signed_at":"2026-10-21T09:15:00Z","certificate":{
		"serial":"0BFD","type":"legal_entity_head",
		"subject":{"full_name":"Кедрова Анна Викторовна","inn":"770586906621"},
		"organization":{"name":"ООО «Кедровый контур Демо»","inn":"7705005410"}}}`)
	cert, err := verify()
	want := companydomain.Certificate{Serial: "0BFD", Type: companydomain.CertificateLegalEntityHead, OwnerINN: "770586906621", OrganizationINN: "7705005410"}
	if err != nil || cert != want {
		t.Fatalf("valid signature: %+v, %v", cert, err)
	}

	var signatureErr *companyerrors.SignatureError
	for name, tt := range map[string]struct {
		answer func(http.ResponseWriter, *http.Request)
		reason string
		want   error
	}{
		"rejected":  {reply(200, `{"valid":false,"reason":"certificate_revoked","certificate":{"serial":"8FA2"}}`), "certificate_revoked", nil},
		"malformed": {reply(400, `{"error":{"code":"MALFORMED_SIGNATURE","message":"Signature is malformed"}}`), "malformed", nil},
		"down":      {reply(500, `{"error":{"code":"INTERNAL_ERROR","message":"boom"}}`), "", companyerrors.ErrESignUnavailable},
		"no cert":   {reply(200, `{"valid":true}`), "", companyerrors.ErrESignUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			answer = tt.answer
			_, err := verify()
			if tt.want != nil {
				if !errors.Is(err, tt.want) {
					t.Fatalf("err = %v, want %v", err, tt.want)
				}
				return
			}
			if !errors.As(err, &signatureErr) || signatureErr.Reason != tt.reason {
				t.Fatalf("err = %v, want SignatureError %q", err, tt.reason)
			}
		})
	}
}
