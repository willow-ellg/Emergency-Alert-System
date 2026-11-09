// Package view holds template helpers shared by the hub and the snapshot
// renderer so both surfaces stay visually and semantically consistent.
package view

import (
	"fmt"
	"html/template"
	"strings"
	"time"

	"emergency-alert-system/internal/model"
)

// FuncMap returns the helper functions used by every HTML template. The now
// function lets a caller pin relative times (for example, a snapshot's
// generation time) without changing the template set.
func FuncMap(now func() time.Time) template.FuncMap {
	return template.FuncMap{
		"severityTag": func(severity model.Severity) string {
			switch severity {
			case model.SeverityCritical:
				return "red"
			case model.SeverityWarning:
				return "yellow"
			default:
				return "blue"
			}
		},
		"severityLabel": func(severity model.Severity) string { return severity.Label() },
		"timeAgo":       func(t time.Time) string { return TimeAgo(now(), t) },
		"formatTime":    func(t time.Time) string { return t.Format("2 January 2006 at 15:04") },
		"join":          strings.Join,
		"distance":      Distance,
		"categoryLabel": CategoryLabel,
		"hasPrefix":     strings.HasPrefix,
	}
}

// TimeAgo renders a short, human-readable relative time.
func TimeAgo(now, then time.Time) string {
	if then.IsZero() {
		return "unknown"
	}
	diff := now.Sub(then)
	if diff < 0 {
		diff = 0
	}
	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		minutes := int(diff / time.Minute)
		if minutes == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", minutes)
	case diff < 24*time.Hour:
		hours := int(diff / time.Hour)
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	default:
		days := int(diff / (24 * time.Hour))
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

// Distance renders a distance in metres or kilometres.
func Distance(km float64) string {
	if km < 1 {
		return fmt.Sprintf("%.0f m", km*1000)
	}
	return fmt.Sprintf("%.1f km", km)
}

// CategoryLabel turns an API category into public-facing copy.
func CategoryLabel(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "hazard":
		return "Hazard"
	case "road":
		return "Road"
	case "shelter":
		return "Shelter"
	case "medical":
		return "Medical"
	case "supplies":
		return "Supplies"
	case "utility":
		return "Utility"
	case "other":
		return "Other"
	default:
		if category == "" {
			return "Update"
		}
		return category
	}
}
