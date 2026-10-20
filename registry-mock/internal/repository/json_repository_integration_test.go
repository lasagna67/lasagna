package repository_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"registry-mock/internal/model"
	"registry-mock/internal/repository"
)

func writeData(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIntegrationJSONRepositoryLookup(t *testing.T) {
	path := writeData(t, `[{"inn":"0123456789","ogrn":"0012345678901","name":{"full":"Demo"},"metadata":{"is_synthetic":true}}]`)
	r, err := repository.NewJSONRepository(path)
	if err != nil {
		t.Fatal(err)
	}
	// Removing the file proves lookups use the startup snapshot.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	company, err := r.GetCompany(context.Background(), "0123456789")
	if err != nil {
		t.Fatal(err)
	}
	if company.INN != "0123456789" || company.OGRN != "0012345678901" || company.Name.Full != "Demo" || !company.Metadata.IsSynthetic {
		t.Fatalf("unexpected company: %+v", company)
	}
	if _, err := r.GetCompany(context.Background(), "9999999999"); !errors.Is(err, model.ErrCompanyNotFound) {
		t.Fatalf("want company-not-found error, got %v", err)
	}
}

func TestIntegrationJSONRepositoryPreservesRussianEntityTypes(t *testing.T) {
	r, err := repository.NewJSONRepository(writeData(t, `[
		{"inn":"7705005410","entity_type":"ЮЛ","source":"ЕГРЮЛ"},
		{"inn":"012345678901","entity_type":"ИП","source":"ЕГРИП"}
	]`))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		inn, entityType, source string
	}{
		{"7705005410", "ЮЛ", "ЕГРЮЛ"},
		{"012345678901", "ИП", "ЕГРИП"},
	} {
		t.Run(tt.entityType, func(t *testing.T) {
			company, err := r.GetCompany(context.Background(), tt.inn)
			if err != nil {
				t.Fatal(err)
			}
			if company.EntityType != tt.entityType || company.Source != tt.source {
				t.Fatalf("unexpected entity type or source: %+v", company)
			}
		})
	}
}

func TestIntegrationJSONRepositoryRejectsBadData(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"invalid JSON", `[{`},
		{"object instead of array", `{}`},
		{"null instead of array", `null`},
		{"trailing JSON", `[] []`},
		{"duplicate INN", `[{"inn":"7705005410"},{"inn":"7705005410"}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := repository.NewJSONRepository(writeData(t, tt.data)); err == nil {
				t.Fatal("expected startup load error")
			}
		})
	}
}

func TestIntegrationJSONRepositoryMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	_, err := repository.NewJSONRepository(path)
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), path) {
		t.Fatalf("want wrapped file error with path, got %v", err)
	}
}

func TestIntegrationJSONRepositoryEmptyArray(t *testing.T) {
	r, err := repository.NewJSONRepository(writeData(t, `[]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetCompany(context.Background(), "7705005410"); !errors.Is(err, model.ErrCompanyNotFound) {
		t.Fatalf("want company-not-found error, got %v", err)
	}
}

func TestIntegrationJSONRepositoryCancelledContext(t *testing.T) {
	r, err := repository.NewJSONRepository(writeData(t, `[{"inn":"7705005410"}]`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.GetCompany(ctx, "7705005410"); !errors.Is(err, context.Canceled) {
		t.Fatalf("want cancellation error, got %v", err)
	}
}
