package main

import (
	"reflect"
	"testing"
)

// TestLoadConfig проверяет настройки по умолчанию и переменные окружения.
func TestLoadConfig(t *testing.T) {
	for _, tt := range []struct {
		name, port, path, secret, origins string
		want                              config
	}{
		{"defaults", "", "", "", "", config{":8084", "testdata/certificates.json", defaultKeySecret, []string{"*"}}},
		{"overrides", "9094", "custom.json", "s3cret", "http://a.ru, http://b.ru",
			config{":9094", "custom.json", "s3cret", []string{"http://a.ru", "http://b.ru"}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("PORT", tt.port)
			t.Setenv("ESIGN_DATA_PATH", tt.path)
			t.Setenv("ESIGN_KEY_SECRET", tt.secret)
			t.Setenv("ALLOWED_ORIGINS", tt.origins)
			if got := loadConfig(); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("loadConfig() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
