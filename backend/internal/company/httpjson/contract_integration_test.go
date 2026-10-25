package companyhttpjson

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"
	"time"

	companyerrors "github.com/lasagna67/lasagna.git/internal/company/errors"
)

// TEST_ESIGN_URL указывает на отдельный esign-mock с тестовым сертификатом A1.
func TestContractSignatureRealMock(t *testing.T) {
	url := os.Getenv("TEST_ESIGN_URL")
	if url == "" {
		t.Skip("TEST_ESIGN_URL is not configured")
	}
	data := "LASAGNA-CREDIT-CONTRACT-V1\napplication_id=application-1\noffer_id=offer-1\ndocument_id=document-1\ndocument_sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\nsigning_nonce=contract-test-nonce"
	body, _ := json.Marshal(map[string]string{"certificate_serial": "A1", "pin": "12345678", "data": data})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/v1/sign", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var signed struct {
		Signature string `json:"signature"`
	}
	if err = json.NewDecoder(response.Body).Decode(&signed); err != nil || response.StatusCode != 200 || signed.Signature == "" {
		t.Fatalf("mock sign failed: status=%d err=%v", response.StatusCode, err)
	}
	client := New(Endpoint{}, Endpoint{URL: url, Timeout: time.Second})
	cert, err := client.VerifySignature(ctx, data, signed.Signature)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Serial != "A1" || cert.Status != "valid" || cert.ValidFrom == "" || cert.ValidTo == "" {
		t.Fatalf("certificate metadata missing: %+v", cert)
	}
	_, err = client.VerifySignature(ctx, data+"changed", signed.Signature)
	var invalid *companyerrors.SignatureError
	if !errors.As(err, &invalid) || invalid.Reason != "signature_mismatch" {
		t.Fatalf("changed contract accepted: %v", err)
	}
}
