package cluster

import (
	"regexp"
	"strings"

	"github.com/dariomory/nomadwifi/pkg/wifi"
)

// Cluster represents a group of related SSIDs and BSSIDs belonging to the same venue/hotel.
type Cluster struct {
	BaseName     string             `json:"base_name"`
	AccessPoints []wifi.AccessPoint `json:"access_points"`
	BestAP       wifi.AccessPoint   `json:"best_ap"`
}

var suffixPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)[-_ ]*(5g|5ghz|2\.4g|2\.4ghz|2g|guest|lobby)$`),
	regexp.MustCompile(`(?i)[-_ ]*floor\s*\d+.*$`),
	regexp.MustCompile(`(?i)[-_ ]*\d+f$`),
	regexp.MustCompile(`(?i)[-_ ]*floor$`),
	regexp.MustCompile(`\d+$`),
}

// NormalizeSSID extracts the core venue / hotel name from an SSID.
// e.g. "SMFLoor21_5G" -> "SM" or "SMFLoor"
// e.g. "BaanNT_2.4G" -> "BaanNT"
func NormalizeSSID(ssid string) string {
	clean := strings.TrimSpace(ssid)
	if clean == "" || clean == "[Hidden SSID]" {
		return ""
	}

	for {
		changed := false
		for _, pattern := range suffixPatterns {
			if pattern.MatchString(clean) {
				newClean := strings.TrimRight(pattern.ReplaceAllString(clean, ""), "-_ ")
				if len(newClean) >= 2 && newClean != clean {
					clean = newClean
					changed = true
					break
				}
			}
		}
		if !changed {
			break
		}
	}

	if len(clean) < 2 {
		return strings.TrimSpace(ssid)
	}
	return clean
}


// GroupClusters groups discovered APs into venue clusters and finds the highest-quality AP in each.
func GroupClusters(aps []wifi.AccessPoint) []Cluster {
	grouped := make(map[string][]wifi.AccessPoint)

	for _, ap := range aps {
		norm := NormalizeSSID(ap.SSID)
		if norm == "" {
			norm = ap.SSID
		}
		grouped[norm] = append(grouped[norm], ap)
	}

	var clusters []Cluster
	for baseName, group := range grouped {
		if len(group) == 0 {
			continue
		}
		best := group[0]
		for _, ap := range group {
			if ap.QualityScore > best.QualityScore {
				best = ap
			}
		}
		clusters = append(clusters, Cluster{
			BaseName:     baseName,
			AccessPoints: group,
			BestAP:       best,
		})
	}

	return clusters
}

// FindAlternativeInCluster finds a significantly better AP in the same venue/hotel cluster.
func FindAlternativeInCluster(currentSSID string, aps []wifi.AccessPoint, minScoreDiff float64) (*wifi.AccessPoint, string) {
	normCurrent := NormalizeSSID(currentSSID)
	if normCurrent == "" {
		return nil, ""
	}

	var currentScore float64
	for _, ap := range aps {
		if strings.EqualFold(ap.SSID, currentSSID) {
			currentScore = ap.QualityScore
			break
		}
	}

	var candidate *wifi.AccessPoint
	for i := range aps {
		ap := &aps[i]
		if strings.EqualFold(ap.SSID, currentSSID) {
			continue
		}
		normCandidate := NormalizeSSID(ap.SSID)
		if strings.EqualFold(normCurrent, normCandidate) {
			if ap.QualityScore > currentScore+minScoreDiff {
				if candidate == nil || ap.QualityScore > candidate.QualityScore {
					candidate = ap
				}
			}
		}
	}

	if candidate != nil {
		reason := "Faster 5 GHz / Wi-Fi standard available in the same hotel cluster"
		if candidate.Band == wifi.Band5GHz && strings.Contains(currentSSID, "2.4") {
			reason = "Upgrade from congested 2.4 GHz to clean 5 GHz band"
		}
		return candidate, reason
	}

	return nil, ""
}
