package main

import "testing"

// TestLoadConfig проверяет настройки по умолчанию и переменные окружения.
func TestLoadConfig(t *testing.T) {
	for _, tt := range []struct{ name, port, path, wantAddress, wantPath string }{
		{"defaults", "", "", ":8083", "testdata/financials.json"},
		{"overrides", "9093", "custom/financials.json", ":9093", "custom/financials.json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PORT", tt.port)
			t.Setenv("FINANCIAL_DATA_PATH", tt.path)
			got := loadConfig()
			if got.Address != tt.wantAddress || got.DataPath != tt.wantPath {
				t.Fatalf("unexpected configuration: %+v", got)
			}
		})
	}
}
