package domain

import (
	"context"
	"testing"
)

func TestGetClaims(t *testing.T) {
	if _, ok := GetClaims(context.Background()); ok {
		t.Error("claims found in empty context")
	}

	want := Claims{UserID: "u1", Role: RoleUnderwriter}
	got, ok := GetClaims(WithClaims(context.Background(), want))
	if !ok || got != want {
		t.Errorf("GetClaims = %+v, %v; want %+v, true", got, ok, want)
	}
}
