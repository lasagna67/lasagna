package main

import "testing"

// TestLoadConfig проверяет настройки по умолчанию и переменные окружения.
func TestLoadConfig(t *testing.T) {
	for _, tt := range []struct{ name, port, path, wantAddress, wantPath string }{
		{"defaults", "", "", ":8082", "testdata/bki.json"},
		{"overrides", "9092", "custom/bki.json", ":9092", "custom/bki.json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PORT", tt.port)
			t.Setenv("BKI_DATA_PATH", tt.path)
			got := loadConfig()
			if got.Address != tt.wantAddress || got.DataPath != tt.wantPath {
				t.Fatalf("unexpected configuration: %+v", got)
			}
		})
	}
}
