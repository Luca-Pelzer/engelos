package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthProviders(t *testing.T) {
	cases := []struct {
		name    string
		flags   AuthProviderFlags
		twitch  bool
		discord bool
	}{
		{"both off", AuthProviderFlags{}, false, false},
		{"twitch only", AuthProviderFlags{Twitch: true}, true, false},
		{"both on", AuthProviderFlags{Twitch: true, Discord: true}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/providers", nil)
			AuthProviders(tc.flags)(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var got map[string]bool
			if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got["twitch"] != tc.twitch {
				t.Errorf("twitch = %v, want %v", got["twitch"], tc.twitch)
			}
			if got["discord"] != tc.discord {
				t.Errorf("discord = %v, want %v", got["discord"], tc.discord)
			}
		})
	}
}
