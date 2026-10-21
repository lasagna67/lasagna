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

const egripData = `[{"inn":"770123456789","status":"ok","report":{"business_type":"ИП","name":"ИП Петров Иван Сергеевич","registration_date":"2019-04-15","business_status":"active","okved":[{"code":"47.91","name":"Торговля розничная по почте или по информационно-коммуникационной сети Интернет"}]}}]`
const egrulData = `[{"inn":"7701234567","status":"ok","report":{"business_type":"ООО","name":"ООО «Ромашка»","registration_date":"2018-06-20","business_status":"active","okved":[{"code":"46.90","name":"Торговля оптовая неспециализированная"}]}}]`

func writeData(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIntegrationJSONRepositoryLookup(t *testing.T) {
	egripPath := writeData(t, egripData)
	egrulPath := writeData(t, egrulData)
	r, err := repository.NewJSONRepository(egripPath, egrulPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{egripPath, egrulPath} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	ip, err := r.GetEGRIP(context.Background(), "770123456789")
	if err != nil || ip.INN != "770123456789" || ip.Status != "ok" || ip.Report.BusinessType != "ИП" || ip.Report.Name != "ИП Петров Иван Сергеевич" || ip.Report.OKVED[0].Code != "47.91" {
		t.Fatalf("unexpected EGRIP report: %+v, %v", ip, err)
	}
	ul, err := r.GetEGRUL(context.Background(), "7701234567")
	if err != nil || ul.Report.BusinessType != "ООО" || ul.Report.Name != "ООО «Ромашка»" {
		t.Fatalf("unexpected EGRUL report: %+v, %v", ul, err)
	}
	for _, lookup := range []func(context.Context, string) (model.Company, error){r.GetEGRIP, r.GetEGRUL} {
		if _, err := lookup(context.Background(), "9999999999"); !errors.Is(err, model.ErrCompanyNotFound) {
			t.Fatalf("want company-not-found error, got %v", err)
		}
	}
	if _, err := r.GetEGRUL(context.Background(), "770123456789"); !errors.Is(err, model.ErrCompanyNotFound) {
		t.Fatalf("EGRIP record leaked into EGRUL: %v", err)
	}
	if _, err := r.GetEGRIP(context.Background(), "7701234567"); !errors.Is(err, model.ErrCompanyNotFound) {
		t.Fatalf("EGRUL record leaked into EGRIP: %v", err)
	}
}

func TestIntegrationJSONRepositoryRejectsBadData(t *testing.T) {
	for _, data := range []string{`[{`, `{}`, `null`, `[] []`, `[{"inn":"7701234567"},{"inn":"7701234567"}]`} {
		for _, source := range []string{"egrip", "egrul"} {
			t.Run(source+"/"+data, func(t *testing.T) {
				egripPath, egrulPath := writeData(t, `[]`), writeData(t, `[]`)
				if source == "egrip" {
					egripPath = writeData(t, data)
				} else {
					egrulPath = writeData(t, data)
				}
				if _, err := repository.NewJSONRepository(egripPath, egrulPath); err == nil {
					t.Fatal("expected startup load error")
				}
			})
		}
	}
}

func TestIntegrationJSONRepositoryDuplicateAcrossFiles(t *testing.T) {
	_, err := repository.NewJSONRepository(writeData(t, egripData), writeData(t, egripData))
	if err == nil || !strings.Contains(err.Error(), "duplicate INN") {
		t.Fatalf("want duplicate INN error, got %v", err)
	}
}

func TestIntegrationJSONRepositoryMissingFile(t *testing.T) {
	for _, source := range []string{"egrip", "egrul"} {
		t.Run(source, func(t *testing.T) {
			missing := filepath.Join(t.TempDir(), "missing.json")
			egripPath, egrulPath := writeData(t, `[]`), writeData(t, `[]`)
			if source == "egrip" {
				egripPath = missing
			} else {
				egrulPath = missing
			}
			_, err := repository.NewJSONRepository(egripPath, egrulPath)
			if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), missing) {
				t.Fatalf("want wrapped file error with path, got %v", err)
			}
		})
	}
}

func TestIntegrationJSONRepositoryEmptyArrays(t *testing.T) {
	r, err := repository.NewJSONRepository(writeData(t, `[]`), writeData(t, `[]`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.GetEGRIP(context.Background(), "770123456789"); !errors.Is(err, model.ErrCompanyNotFound) {
		t.Fatalf("want company-not-found error, got %v", err)
	}
}

func TestIntegrationJSONRepositoryCancelledContext(t *testing.T) {
	r, err := repository.NewJSONRepository(writeData(t, egripData), writeData(t, egrulData))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, lookup := range []func(context.Context, string) (model.Company, error){r.GetEGRIP, r.GetEGRUL} {
		if _, err := lookup(ctx, "770123456789"); !errors.Is(err, context.Canceled) {
			t.Fatalf("want cancellation error, got %v", err)
		}
	}
}
