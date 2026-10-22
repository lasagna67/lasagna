package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"esign-mock/internal/model"
)

const validCertificate = `{"serial":"A1","type":"individual_entrepreneur",
  "subject":{"full_name":"Петров Иван Сергеевич","inn":"770123456789"},"organization":null,
  "issuer":"УЦ","valid_from":"2026-01-01","valid_to":"2027-01-01","status":"valid","pin":"1"}`

func writeData(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "certificates.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIntegrationJSONRepositoryLookup(t *testing.T) {
	r, err := NewJSONRepository(writeData(t, "["+validCertificate+"]"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Get(context.Background(), "A1")
	if err != nil || got.PIN != "1" || got.Subject.INN != "770123456789" {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	if _, err := r.Get(context.Background(), "B2"); !errors.Is(err, model.ErrCertificateNotFound) {
		t.Fatalf("missing serial: %v", err)
	}
}

func TestIntegrationJSONRepositoryRejectsBadData(t *testing.T) {
	broken := func(old, new string) string { return "[" + strings.Replace(validCertificate, old, new, 1) + "]" }
	for name, data := range map[string]string{
		"not JSON":          "{",
		"null":              "null",
		"duplicate serial":  "[" + validCertificate + "," + validCertificate + "]",
		"short INN":         broken(`"770123456789"`, `"7701"`),
		"unknown type":      broken(`"individual_entrepreneur"`, `"robot"`),
		"unknown status":    broken(`"valid"`, `"maybe"`),
		"bad date":          broken(`"2027-01-01"`, `"01.01.2027"`),
		"reversed validity": broken(`"2027-01-01"`, `"2025-01-01"`),
		"missing PIN":       broken(`"pin":"1"`, `"pin":""`),
		"head without org":  broken(`"individual_entrepreneur"`, `"legal_entity_head"`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewJSONRepository(writeData(t, data)); err == nil {
				t.Fatal("bad data accepted")
			}
		})
	}
}

func TestIntegrationJSONRepositoryShippedData(t *testing.T) {
	r, err := NewJSONRepository("../../testdata/certificates.json")
	if err != nil {
		t.Fatalf("shipped certificates do not load: %v", err)
	}
	list, err := r.List(context.Background())
	if err != nil || len(list) == 0 {
		t.Fatalf("List = %d, %v", len(list), err)
	}
}
