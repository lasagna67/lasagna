package companyhttpjson

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

type reportTransport func(*http.Request) (*http.Response, error)

func (f reportTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReports(t *testing.T) {
	for _, kind := range []string{"credit", "financial"} {
		for _, tc := range []struct {
			name, body    string
			code          int
			missing, fail bool
		}{
			{"ok", `{"inn":"7705005410","status":"ok","report":{"loans":[{"amount":9007199254740993}]},"monthly_data":[{"income":9007199254740993}]}`, 200, false, false},
			{"missing", `{"error":{"code":"CREDIT_HISTORY_NOT_FOUND"}}`, 404, true, true},
			{"unavailable", `{"error":{"message":"private error"}}`, 503, false, true},
			{"wrong route", `{"error":{"code":"NOT_FOUND"}}`, 404, false, true},
			{"invalid JSON", `{`, 200, false, true},
			{"wrong INN", `{"inn":"1234567890","status":"ok"}`, 200, false, true},
			{"no data", `{"inn":"7705005410","status":"no_data"}`, 200, false, true},
		} {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				body := tc.body
				if kind == "financial" {
					body = strings.ReplaceAll(body, "CREDIT_HISTORY_NOT_FOUND", "FINANCIAL_DATA_NOT_FOUND")
				}
				transport := reportTransport(func(r *http.Request) (*http.Response, error) {
					path := "/v1/credit-reports/7705005410"
					if kind == "financial" {
						path = "/v1/financial-reports/7705005410"
					}
					if r.URL.Path != path || r.Method != "GET" {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					}
					return &http.Response{StatusCode: tc.code, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
				})
				a := api{baseURL: "http://unit.test", http: &http.Client{Transport: transport}}
				c := &Client{bki: a, financials: a}
				var err error
				if kind == "credit" {
					var reportAmount int64
					report, e := c.GetCreditReport(context.Background(), "7705005410")
					err = e
					if e == nil {
						reportAmount = report.Report.Loans[0].Amount
						if reportAmount != 9007199254740993 {
							t.Fatal("lost precision")
						}
					}
				} else {
					report, e := c.GetFinancialReport(context.Background(), "7705005410")
					err = e
					if e == nil && report.MonthlyData[0].Income != 9007199254740993 {
						t.Fatal("lost precision")
					}
				}
				missing := tc.missing || (kind == "financial" && tc.name == "no data")
				if (err != nil) != tc.fail {
					t.Fatalf("error=%v, want failure=%v", err, tc.fail)
				}
				if errors.Is(err, companyerrors.ErrReportNotFound) != missing {
					t.Fatalf("missing classification: %v", err)
				}
			})
		}
	}
}

func TestReportTransportFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a := api{baseURL: "http://unit.test", http: &http.Client{Transport: reportTransport(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })}}
	c := &Client{bki: a, financials: a}
	if _, err := c.GetCreditReport(ctx, "7705005410"); !errors.Is(err, context.Canceled) {
		t.Fatalf("credit cancellation: %v", err)
	}
	if _, err := c.GetFinancialReport(ctx, "7705005410"); !errors.Is(err, context.Canceled) {
		t.Fatalf("financial cancellation: %v", err)
	}
}
