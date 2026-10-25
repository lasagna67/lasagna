package handler_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestContractContextSignature(t *testing.T) {
	routes := newRoutes(t)
	data := "LASAGNA-CREDIT-CONTRACT-V1\napplication_id=application-1\noffer_id=offer-1\ndocument_id=document-1\ndocument_sha256=" + strings.Repeat("a", 64) + "\nsigning_nonce=random-nonce"
	body, _ := json.Marshal(map[string]string{"certificate_serial": "A1", "pin": "12345678", "data": data})
	response := do(t, routes, http.MethodPost, "/v1/sign", string(body))
	if response.Code != 200 {
		t.Fatalf("sign: %d %s", response.Code, response.Body)
	}
	signature := decode(t, response.Body.Bytes())["signature"].(string)
	for _, tc := range []struct {
		name, data string
		valid      bool
	}{
		{"original", data, true},
		{"offer", strings.Replace(data, "offer-1", "offer-2", 1), false},
		{"document", strings.Replace(data, "document-1", "document-2", 1), false},
		{"hash", strings.Replace(data, strings.Repeat("a", 64), strings.Repeat("b", 64), 1), false},
		{"nonce", strings.Replace(data, "random-nonce", "another-nonce", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"data": tc.data, "signature": signature})
			response := do(t, routes, http.MethodPost, "/v1/verify", string(body))
			if response.Code != 200 || decode(t, response.Body.Bytes())["valid"] != tc.valid {
				t.Fatalf("verify: %d %s", response.Code, response.Body)
			}
		})
	}
}
