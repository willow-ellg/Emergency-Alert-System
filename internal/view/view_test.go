package view

import (
	"testing"
	"time"
)

func TestTimeAgo(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		then time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-time.Minute), "1 minute ago"},
		{now.Add(-5 * time.Minute), "5 minutes ago"},
		{now.Add(-time.Hour), "1 hour ago"},
		{now.Add(-3 * time.Hour), "3 hours ago"},
		{now.Add(-24 * time.Hour), "1 day ago"},
		{now.Add(-72 * time.Hour), "3 days ago"},
		{time.Time{}, "unknown"},
		{now.Add(time.Hour), "just now"},
	}
	for _, test := range tests {
		if got := TimeAgo(now, test.then); got != test.want {
			t.Errorf("TimeAgo(%v) = %q, want %q", test.then, got, test.want)
		}
	}
}

func TestDistance(t *testing.T) {
	tests := []struct {
		km   float64
		want string
	}{
		{0.25, "250 m"},
		{0.999, "999 m"},
		{1, "1.0 km"},
		{12.345, "12.3 km"},
	}
	for _, test := range tests {
		if got := Distance(test.km); got != test.want {
			t.Errorf("Distance(%v) = %q, want %q", test.km, got, test.want)
		}
	}
}

func TestCategoryLabel(t *testing.T) {
	tests := map[string]string{
		"hazard":  "Hazard",
		" ROAD ":  "Road",
		"medical": "Medical",
		"other":   "Other",
		"":        "Update",
		"custom":  "custom",
	}
	for input, want := range tests {
		if got := CategoryLabel(input); got != want {
			t.Errorf("CategoryLabel(%q) = %q, want %q", input, got, want)
		}
	}
}
