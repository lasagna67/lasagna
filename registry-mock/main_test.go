package main

import (
	"testing"
)

func TestLoadConfig(t *testing.T) {
	for _, tt := range []struct {
		name, port, path, wantAddress, wantPath string
	}{
		{"defaults", "", "", ":8081", "testdata/companies.json"},
		{"overrides", "9091", "custom/registry.json", ":9091", "custom/registry.json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PORT", tt.port)
			t.Setenv("REGISTRY_DATA_PATH", tt.path)
			got := loadConfig()
			if got.Address != tt.wantAddress || got.DataPath != tt.wantPath {
				t.Fatalf("unexpected configuration: %+v", got)
			}
		})
	}
}
