// Package model contains the domain types shared by the full hub and the
// thin emergency snapshot.
package model

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Severity describes how urgently the public should act on an alert.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

// ParseSeverity validates a severity supplied by an operator or API client.
func ParseSeverity(value string) (Severity, error) {
	switch Severity(strings.ToLower(strings.TrimSpace(value))) {
	case SeverityCritical:
		return SeverityCritical, nil
	case SeverityWarning:
		return SeverityWarning, nil
	case SeverityInfo:
		return SeverityInfo, nil
	default:
		return "", fmt.Errorf("invalid severity %q: use critical, warning or info", value)
	}
}

// Rank gives higher-severity alerts a larger number for sorting.
func (s Severity) Rank() int {
	switch s {
	case SeverityCritical:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// Label is the public-facing name of the severity.
func (s Severity) Label() string {
	switch s {
	case SeverityCritical:
		return "Critical"
	case SeverityWarning:
		return "Warning"
	case SeverityInfo:
		return "Information"
	default:
		return "Information"
	}
}

// Alert is an official or partner-issued emergency message.
type Alert struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Severity  Severity  `json:"severity"`
	Areas     []string  `json:"areas,omitempty"`
	Lat       float64   `json:"lat,omitempty"`
	Lon       float64   `json:"lon,omitempty"`
	RadiusKM  float64   `json:"radius_km,omitempty"`
	Summary   string    `json:"summary"`
	Guidance  []string  `json:"guidance,omitempty"`
	Source    string    `json:"source"`
	Official  bool      `json:"official"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// HasGeo reports whether the alert has a usable geographic radius.
func (a Alert) HasGeo() bool {
	return a.RadiusKM > 0 && (a.Lat != 0 || a.Lon != 0)
}

// HasArea reports whether the alert is limited to named areas.
func (a Alert) HasArea() bool {
	return len(a.Areas) > 0
}

// Matches applies the public location filters. Filters are ORed together:
// an alert is relevant if it names the requested area, is within range of the
// supplied coordinates, or applies nationally (no area and no geo).
func (a Alert) Matches(lat, lon float64, hasLocation bool, area string) bool {
	if area == "" && !hasLocation {
		return true
	}
	if !a.HasArea() && !a.HasGeo() {
		return true
	}
	if area != "" && AreaMatches(a.Areas, area) {
		return true
	}
	if hasLocation && a.HasGeo() && DistanceKM(lat, lon, a.Lat, a.Lon) <= a.RadiusKM {
		return true
	}
	return false
}

// Clone returns a deep copy so callers cannot mutate store state.
func (a Alert) Clone() Alert {
	a.Areas = append([]string(nil), a.Areas...)
	a.Guidance = append([]string(nil), a.Guidance...)
	return a
}

// ReportStatus tracks moderation state for public submissions.
type ReportStatus string

const (
	ReportPending  ReportStatus = "pending"
	ReportApproved ReportStatus = "approved"
	ReportRejected ReportStatus = "rejected"
)

// Report is a public, location-tagged update submitted by a member of the
// public. Reports are never published until an operator approves them.
type Report struct {
	ID         string       `json:"id"`
	AlertID    string       `json:"alert_id,omitempty"`
	Area       string       `json:"area"`
	Category   string       `json:"category"`
	Body       string       `json:"body"`
	Lat        float64      `json:"lat,omitempty"`
	Lon        float64      `json:"lon,omitempty"`
	Status     ReportStatus `json:"status"`
	Verified   bool         `json:"verified"`
	CreatedAt  time.Time    `json:"created_at"`
	ReviewedAt *time.Time   `json:"reviewed_at,omitempty"`
}

// Clone returns a deep copy so callers cannot mutate store state.
func (r Report) Clone() Report {
	if r.ReviewedAt != nil {
		reviewed := *r.ReviewedAt
		r.ReviewedAt = &reviewed
	}
	return r
}

// Myth records a circulating claim and the official correction.
type Myth struct {
	ID         string    `json:"id"`
	Claim      string    `json:"claim"`
	Correction string    `json:"correction"`
	Source     string    `json:"source"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Clone returns a deep copy so callers cannot mutate store state.
func (m Myth) Clone() Myth { return m }

// State is the complete persisted dataset.
type State struct {
	Alerts    []Alert   `json:"alerts"`
	Reports   []Report  `json:"reports"`
	Myths     []Myth    `json:"myths"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Clone returns a deep copy of the whole state.
func (s State) Clone() State {
	out := State{
		Alerts:    make([]Alert, len(s.Alerts)),
		Reports:   make([]Report, len(s.Reports)),
		Myths:     make([]Myth, len(s.Myths)),
		Version:   s.Version,
		UpdatedAt: s.UpdatedAt,
	}
	for i, alert := range s.Alerts {
		out.Alerts[i] = alert.Clone()
	}
	for i, report := range s.Reports {
		out.Reports[i] = report.Clone()
	}
	for i, myth := range s.Myths {
		out.Myths[i] = myth.Clone()
	}
	return out
}

// NewID returns a short, random, prefixed identifier.
func NewID(prefix string) string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	}
	return prefix + "-" + hex.EncodeToString(buf)
}

// NormalizeArea prepares an area string for comparison.
func NormalizeArea(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

// AreaMatches reports whether the query matches one of the alert areas.
// It is deliberately forgiving so "Riverside" matches "Riverside East".
func AreaMatches(areas []string, query string) bool {
	q := NormalizeArea(query)
	if q == "" {
		return true
	}
	for _, area := range areas {
		n := NormalizeArea(area)
		if n == "" {
			continue
		}
		if n == q {
			return true
		}
		if len(q) >= 3 && strings.Contains(n, q) {
			return true
		}
		if len(n) >= 3 && strings.Contains(q, n) {
			return true
		}
	}
	return false
}

// DistanceKM returns the great-circle distance between two points.
func DistanceKM(lat1, lon1, lat2, lon2 float64) float64 {
	const earthRadiusKM = 6371.0
	toRadians := func(degrees float64) float64 { return degrees * math.Pi / 180 }

	dLat := toRadians(lat2 - lat1)
	dLon := toRadians(lon2 - lon1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRadians(lat1))*math.Cos(toRadians(lat2))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return earthRadiusKM * 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// SortAlerts orders alerts by severity and then recency.
func SortAlerts(alerts []Alert) {
	sort.SliceStable(alerts, func(i, j int) bool {
		if alerts[i].Severity.Rank() != alerts[j].Severity.Rank() {
			return alerts[i].Severity.Rank() > alerts[j].Severity.Rank()
		}
		return alerts[i].UpdatedAt.After(alerts[j].UpdatedAt)
	})
}

// SortReports orders reports newest first.
func SortReports(reports []Report) {
	sort.SliceStable(reports, func(i, j int) bool {
		return reports[i].CreatedAt.After(reports[j].CreatedAt)
	})
}
