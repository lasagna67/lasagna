package main

import "testing"

func TestLoadConfig(t *testing.T) {
	for _, tt := range []struct {
		name, port, ipPath, ulPath, wantAddress, wantIPPath, wantULPath string
	}{
		{"defaults", "", "", "", ":8081", "testdata/egrip.json", "testdata/egrul.json"},
		{"overrides", "9091", "custom/ip.json", "custom/ul.json", ":9091", "custom/ip.json", "custom/ul.json"},
		{"only EGRIP override", "", "custom/ip.json", "", ":8081", "custom/ip.json", "testdata/egrul.json"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PORT", tt.port)
			t.Setenv("EGRIP_DATA_PATH", tt.ipPath)
			t.Setenv("EGRUL_DATA_PATH", tt.ulPath)
			got := loadConfig()
			if got.Address != tt.wantAddress || got.EGRIPDataPath != tt.wantIPPath || got.EGRULDataPath != tt.wantULPath {
				t.Fatalf("unexpected configuration: %+v", got)
			}
		})
	}
}
