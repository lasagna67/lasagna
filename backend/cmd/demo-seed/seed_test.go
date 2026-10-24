package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSeedAccount(t *testing.T) {
	for _, financialStatus := range []string{"ok", "not_found", "unavailable"} {
		for _, existing := range []bool{false, true} {
			t.Run(financialStatus+"/"+map[bool]string{false: "new", true: "existing"}[existing], func(t *testing.T) {
				var calls []string
				var accountCalls int
				var balanceCalls int
				transport := transportFunc(func(r *http.Request) (*http.Response, error) {
					calls = append(calls, r.Method+" "+r.URL.Path)
					status, body := 200, `{}`
					switch r.URL.Path {
					case "/api/v0/users":
						status = 201
						if existing {
							status = 409
						}
					case "/api/v0/auth/login":
						body = `{"access_token":"secret-token","user":{"id":"user1","role":"client"}}`
					case "/api/v0/companies":
						if r.Method == "GET" {
							body = `[]`
							if existing {
								body = `[{"id":"company1","inn":"7705005410","name":"Demo"}]`
							}
						} else {
							body = `{"id":"company1","inn":"7705005410","name":"Demo"}`
							status = 201
						}
					case "/api/v0/companies/challenges":
						body = `{"challenge_id":"challenge1","data":"statement\nexact"}`
						status = 201
					case "/v1/sign":
						var request struct {
							Data string `json:"data"`
						}
						if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
							t.Fatal(err)
						}
						if request.Data != "statement\nexact" {
							t.Fatal("statement changed")
						}
						body = `{"signature":"signed"}`
					case "/api/v0/auth/logout":
						status = 204
					case "/api/v0/companies/company1/overview":
						body = `{"financials":{"status":"ok","data":{"inn":"7705005410","status":"ok","currency":"RUB","monthly_data":[{"month":"2026-09","income":100000,"expenses":60000}]}}}`
						if financialStatus != "ok" {
							body = `{"financials":{"status":"` + financialStatus + `","data":null}}`
						}
					default:
						t.Fatalf("unexpected request %s", r.URL.Path)
					}
					if strings.HasPrefix(r.URL.Path, "/api/v0/companies") && r.Header.Get("Authorization") != "Bearer secret-token" {
						t.Fatal("missing token")
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
				})
				s := seeder{client: &http.Client{Transport: transport}, baseURL: "http://backend", esignURL: "http://esign", password: "demo-password", pin: "12345678"}
				s.ensureAccount = func(_ context.Context, companyID string) error {
					if companyID != "company1" {
						t.Fatalf("wrong account company: %s", companyID)
					}
					accountCalls++
					return nil
				}
				s.seedBalance = func(_ context.Context, companyID string, balance int64) error {
					balanceCalls++
					if companyID != "company1" || (financialStatus == "ok" && (balance < 20000 || balance > 60000)) || (financialStatus == "not_found" && balance != 0) {
						t.Fatalf("incorrect seeded balance: %s %d", companyID, balance)
					}
					return nil
				}
				result, err := s.seedAccount(context.Background(), profiles[0], certificate{Serial: "cert1"})
				if err != nil {
					t.Fatal(err)
				}
				if result.CompanyID != "company1" || result.Email != "demo.ooo.good@lasagna.test" {
					t.Fatalf("unexpected result %+v", result)
				}
				if accountCalls != 1 {
					t.Fatalf("account must be ensured for new and existing company, got %d calls", accountCalls)
				}
				wantBalanceCalls := 1
				if financialStatus == "unavailable" {
					wantBalanceCalls = 0
				}
				if balanceCalls != wantBalanceCalls {
					t.Fatalf("balance seed calls: got %d, want %d", balanceCalls, wantBalanceCalls)
				}
				joined := strings.Join(calls, ";")
				if strings.Contains(joined, "/v1/sign") == existing {
					t.Fatal("existing company must not be signed again")
				}
				if calls[len(calls)-1] != "POST /api/v0/auth/logout" {
					t.Fatal("session not closed")
				}
			})
		}
	}
}

func TestCertificateSelection(t *testing.T) {
	certs := []certificate{{Serial: "revoked", Type: "legal_entity_head", Status: "revoked"}, {Serial: "head", Type: "legal_entity_head", Status: "valid"}}
	for i := range certs {
		certs[i].Organization.INN = "7705005410"
		certs[i].Subject.INN = "head-inn"
	}
	got, err := selectCertificate(profiles[0], "head-inn", certs)
	if err != nil || got.Serial != "head" {
		t.Fatalf("certificate: %+v %v", got, err)
	}
	if _, err := selectCertificate(profiles[0], "other-head", certs); err == nil {
		t.Fatal("wrong head accepted")
	}
}

func TestProfileCatalog(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range profiles {
		key := p.Kind + "-" + p.Scenario
		if seen[key] {
			t.Fatalf("duplicate %s", key)
		}
		seen[key] = true
	}
	if len(seen) != 12 {
		t.Fatalf("want 12 profiles, got %d", len(seen))
	}
}

func TestSeedDoesNotResetConflictingPassword(t *testing.T) {
	var calls int
	s := seeder{baseURL: "http://backend", client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		status := 409
		if calls == 2 {
			status = 401
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}}
	if _, err := s.seedAccount(context.Background(), profiles[0], certificate{}); err == nil {
		t.Fatal("wrong password silently accepted")
	}
	if calls != 2 {
		t.Fatalf("unexpected requests after login failure: %d", calls)
	}
}
