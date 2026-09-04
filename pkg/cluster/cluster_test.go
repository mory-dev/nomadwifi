package cluster

import (
	"testing"

	"github.com/mory-dev/nomadwifi/pkg/wifi"
)

func TestNormalizeSSID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"SMFLoor21_5G", "SM"},
		{"SMFLoor21", "SM"},
		{"BaanNT_2.4G", "BaanNT"},
		{"BaanNT_5G", "BaanNT"},
		{"Hotel_Lobby", "Hotel"},
		{"Starbucks_Guest", "Starbucks"},
		{"CoworkSpace-5GHz", "CoworkSpace"},
	}

	for _, tc := range tests {
		got := NormalizeSSID(tc.input)
		if got != tc.expected {
			t.Errorf("NormalizeSSID(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}

func TestFindAlternativeInCluster(t *testing.T) {
	aps := []wifi.AccessPoint{
		{
			SSID:          "SMFLoor21",
			BSSID:         "24:cf:24:e5:73:99",
			Band:          wifi.Band24GHz,
			RadioType:     "802.11g",
			SignalPercent: 86,
			QualityScore:  40.0,
		},
		{
			SSID:          "SMFLoor21_5G",
			BSSID:         "24:cf:24:e5:73:9a",
			Band:          wifi.Band5GHz,
			RadioType:     "802.11ac",
			SignalPercent: 80,
			QualityScore:  82.0,
		},
		{
			SSID:          "UnrelatedCoffeeShop",
			BSSID:         "aa:bb:cc:dd:ee:ff",
			Band:          wifi.Band5GHz,
			RadioType:     "802.11ax",
			SignalPercent: 95,
			QualityScore:  90.0,
		},
	}

	better, reason := FindAlternativeInCluster("SMFLoor21", aps, 10.0)
	if better == nil {
		t.Fatalf("expected to find alternative in cluster, got nil")
	}
	if better.SSID != "SMFLoor21_5G" {
		t.Errorf("expected SMFLoor21_5G, got %s", better.SSID)
	}
	if reason == "" {
		t.Errorf("expected non-empty reason")
	}
}
