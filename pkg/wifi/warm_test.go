package wifi

import (
	"testing"

	"github.com/mory-dev/nomadwifi/pkg/state"
)

func TestWarmCandidatesForVenueUsesConfirmedKeyEvenWhenRowsAreLocked(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	state.Reset()

	aps := []AccessPoint{
		{SSID: "Hotel_5G", Authentication: "WPA2-Personal", Cipher: "CCMP", AuthStatus: AuthStatusLocked},
		{SSID: "Hotel Lobby", Authentication: "WPA2-Personal", Cipher: "CCMP", AuthStatus: AuthStatusLocked},
		{SSID: "Hotel Floor2", Authentication: "WPA2-Personal", Cipher: "CCMP", AuthStatus: AuthStatusLocked},
		{SSID: "Unrelated_5G", Authentication: "WPA2-Personal", Cipher: "CCMP", AuthStatus: AuthStatusLocked},
	}

	got := warmCandidatesForVenue(aps, "Hotel", 2, map[string]bool{"hotel": true})
	if len(got) != 2 {
		t.Fatalf("got %d candidates, want 2", len(got))
	}
	if got[0].SSID != "Hotel_5G" || got[1].SSID != "Hotel Lobby" {
		t.Errorf("candidates = %q, %q; want Hotel_5G, Hotel Lobby", got[0].SSID, got[1].SSID)
	}
}

func TestAnnotateAuthStatusUnlocksRecognizedSiblings(t *testing.T) {
	aps := []AccessPoint{
		{SSID: "Hotel", Authentication: "WPA2-Personal", Cipher: "CCMP"},
		{SSID: "Hotel_5G", Authentication: "WPA2-Personal", Cipher: "CCMP"},
		{SSID: "Unrelated_5G", Authentication: "WPA2-Personal", Cipher: "CCMP"},
	}

	guess := func(ssid string) (string, string) {
		if IsSameHotelVenue("Hotel", ssid) && ssid != "Hotel" {
			return "confirmed-key", "active hotel network 'Hotel'"
		}
		return "", ""
	}
	annotateAuthStatus(aps, map[string]bool{"hotel": true}, guess)

	if aps[0].AuthStatus != AuthStatusSaved {
		t.Errorf("connected network status = %s, want %s", aps[0].AuthStatus, AuthStatusSaved)
	}
	if aps[1].AuthStatus != AuthStatusInferred {
		t.Errorf("sibling status = %s, want %s", aps[1].AuthStatus, AuthStatusInferred)
	}
	if aps[2].AuthStatus != AuthStatusLocked {
		t.Errorf("unrelated status = %s, want %s", aps[2].AuthStatus, AuthStatusLocked)
	}
}
