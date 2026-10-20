package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegrationRunReportsMissingDataBeforeServing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	t.Setenv("REGISTRY_DATA_PATH", path)
	err := run()
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "initialize registry") {
		t.Fatalf("want clear startup load error, got %v", err)
	}
}
