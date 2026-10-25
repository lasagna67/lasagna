package intakehandler

import (
	"encoding/json"
	"net/http"
	"testing"

	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	applicationdto "github.com/lasagna67/lasagna.git/internal/application/dto"
	applicationerrors "github.com/lasagna67/lasagna.git/internal/application/errors"
)

func TestGetApplication(t *testing.T) {
	svc := &fakeService{}
	e := newTestServer(client, svc)
	rec := do(e, http.MethodGet, "/applications/"+appID.String(), "")
	var body struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.ID != appID.String() || body.Status != "draft" {
		t.Fatalf("get = %d %s", rec.Code, rec.Body)
	}
	if rec := do(e, http.MethodGet, "/applications/not-a-uuid", ""); rec.Code != http.StatusNotFound {
		t.Errorf("bad id = %d", rec.Code)
	}
	svc.err = applicationerrors.ErrNotFound
	if rec := do(e, http.MethodGet, "/applications/"+appID.String(), ""); rec.Code != http.StatusNotFound {
		t.Errorf("foreign = %d", rec.Code)
	}
	if rec := do(newTestServer(nil, svc), http.MethodGet, "/applications/"+appID.String(), ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token = %d", rec.Code)
	}
}

func TestListApplications(t *testing.T) {
	svc := &fakeService{}
	e := newTestServer(client, svc)
	cursor := applicationdto.EncodeCursor(1005)
	rec := do(e, http.MethodGet, "/applications?company_id="+companyID.String()+"&status=manual_review&limit=5&cursor="+cursor, "")
	var page struct {
		Items      []struct{ ID string } `json:"items"`
		NextCursor *string               `json:"next_cursor"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
	if before, err := applicationdto.DecodeCursor(*page.NextCursor); err != nil || before != 1000 {
		t.Errorf("next cursor = %s (%d, %v)", *page.NextCursor, before, err)
	}
	c := svc.list
	if c.UserID != client.UserID || c.CompanyID != companyID || c.Status == nil || *c.Status != applicationdomain.StatusManualReview ||
		c.Limit != 5 || c.Before == nil || *c.Before != 1005 {
		t.Errorf("command = %+v", c)
	}

	for _, q := range []string{"", "?company_id=nope", "?company_id=" + companyID.String() + "&limit=0", "?company_id=" + companyID.String() + "&cursor=***"} {
		if rec := do(e, http.MethodGet, "/applications"+q, ""); rec.Code != http.StatusBadRequest {
			t.Errorf("GET /applications%s = %d; want 400", q, rec.Code)
		}
	}
	svc.err = applicationerrors.ErrCompanyNotFound
	if rec := do(e, http.MethodGet, "/applications?company_id="+companyID.String(), ""); rec.Code != http.StatusNotFound {
		t.Errorf("foreign company = %d", rec.Code)
	}
}
