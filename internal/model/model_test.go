package model

import (
	"math"
	"testing"
	"time"
)

func TestParseSeverity(t *testing.T) {
	tests := []struct {
		input string
		want  Severity
		ok    bool
	}{
		{"critical", SeverityCritical, true},
		{" WARNING ", SeverityWarning, true},
		{"Info", SeverityInfo, true},
		{"", "", false},
		{"emergency", "", false},
	}
	for _, test := range tests {
		got, err := ParseSeverity(test.input)
		if test.ok && err != nil {
			t.Fatalf("ParseSeverity(%q) returned error: %v", test.input, err)
		}
		if !test.ok && err == nil {
			t.Fatalf("ParseSeverity(%q) expected an error", test.input)
		}
		if got != test.want {
			t.Fatalf("ParseSeverity(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestSeverityRankAndLabel(t *testing.T) {
	if SeverityCritical.Rank() <= SeverityWarning.Rank() || SeverityWarning.Rank() <= SeverityInfo.Rank() {
		t.Fatal("severity ranks are not ordered")
	}
	if SeverityInfo.Label() != "Information" {
		t.Fatalf("SeverityInfo.Label() = %q", SeverityInfo.Label())
	}
}

func TestAlertMatches(t *testing.T) {
	national := Alert{ID: "national"}
	area := Alert{ID: "area", Areas: []string{"Riverside East"}}
	geo := Alert{ID: "geo", Lat: 51.5, Lon: -0.1, RadiusKM: 10}
	both := Alert{ID: "both", Areas: []string{"Northgate"}, Lat: 51.5, Lon: -0.1, RadiusKM: 10}

	tests := []struct {
		name        string
		alert       Alert
		lat, lon    float64
		hasLocation bool
		area        string
		want        bool
	}{
		{"no filter shows every alert", geo, 0, 0, false, "", true},
		{"national alert matches any area", national, 0, 0, false, "Riverside", true},
		{"named area matches substring", area, 0, 0, false, "Riverside", true},
		{"different area does not match", area, 0, 0, false, "Northgate", false},
		{"geo alert matches nearby point", geo, 51.501, -0.1, true, "", true},
		{"geo alert does not match distant point", geo, 52.5, -0.1, true, "", false},
		{"area wins over distant point", both, 52.5, -0.1, true, "Northgate", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.alert.Matches(test.lat, test.lon, test.hasLocation, test.area); got != test.want {
				t.Fatalf("Matches() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestAreaMatchesIsForgiving(t *testing.T) {
	areas := []string{"Riverside East", "Lowbridge"}
	if !AreaMatches(areas, "riverside") {
		t.Fatal("expected case-insensitive substring match")
	}
	if !AreaMatches(areas, "  LOWBRIDGE ") {
		t.Fatal("expected whitespace and case normalization")
	}
	if AreaMatches(areas, "Harbour") {
		t.Fatal("unexpected match")
	}
	if !AreaMatches(areas, "") {
		t.Fatal("empty query should match")
	}
}

func TestDistanceKM(t *testing.T) {
	if got := DistanceKM(51.5, -0.1, 51.5, -0.1); got != 0 {
		t.Fatalf("DistanceKM to same point = %f", got)
	}
	// London to Manchester is roughly 262 km.
	got := DistanceKM(51.5074, -0.1278, 53.4808, -2.2426)
	if math.Abs(got-262) > 10 {
		t.Fatalf("DistanceKM London to Manchester = %.1f km, want about 262 km", got)
	}
}

func TestSortAlerts(t *testing.T) {
	alerts := []Alert{
		{ID: "info", Severity: SeverityInfo},
		{ID: "critical-old", Severity: SeverityCritical},
		{ID: "warning", Severity: SeverityWarning},
		{ID: "critical-new", Severity: SeverityCritical},
	}
	now := time.Now()
	alerts[1].UpdatedAt = now.Add(-time.Minute)
	alerts[3].UpdatedAt = now
	SortAlerts(alerts)
	if alerts[0].ID != "critical-new" || alerts[1].ID != "critical-old" {
		t.Fatalf("critical alerts not ordered by recency: %+v", alerts)
	}
	if alerts[2].ID != "warning" || alerts[3].ID != "info" {
		t.Fatalf("alerts not ordered by severity: %+v", alerts)
	}
}

func TestCloneIsDeep(t *testing.T) {
	alert := Alert{Areas: []string{"A"}, Guidance: []string{"B"}}
	clone := alert.Clone()
	clone.Areas[0] = "changed"
	clone.Guidance[0] = "changed"
	if alert.Areas[0] != "A" || alert.Guidance[0] != "B" {
		t.Fatal("Clone shared slices with the original")
	}
}
