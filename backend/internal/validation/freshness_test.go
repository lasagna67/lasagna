package validation

import (
	"reflect"
	"testing"
	"time"
)

func TestDataCurrent(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		date *time.Time
		want *bool
	}{
		{"missing", nil, nil},
		{"now", timeValue(now), boolValue(true)},
		{"30 days inclusive", timeValue(now.Add(-30 * 24 * time.Hour)), boolValue(true)},
		{"expired", timeValue(now.Add(-30*24*time.Hour - time.Nanosecond)), boolValue(false)},
		{"future", timeValue(now.Add(time.Nanosecond)), boolValue(false)},
		{"zero", timeValue(time.Time{}), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := DataCurrent(tc.date, now, 30*24*time.Hour); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func timeValue(t time.Time) *time.Time { return &t }
func boolValue(value bool) *bool       { return &value }
