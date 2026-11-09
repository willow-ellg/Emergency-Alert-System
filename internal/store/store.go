// Package store provides a small, dependency-free JSON datastore.
//
// The store is deliberately boring: one file, atomic replacement, and a
// mutex. That keeps the full hub easy to run on a single emergency-response
// host and makes the on-disk state portable to the thin snapshot tooling.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"emergency-alert-system/internal/model"
)

// ErrNotFound is returned when an entity does not exist.
var ErrNotFound = errors.New("not found")

// Store is a concurrency-safe, file-backed datastore.
type Store struct {
	mu    sync.RWMutex
	path  string
	state model.State
}

// New loads the store at path. A missing file starts an empty store.
func New(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, fmt.Errorf("read store: %w", err)
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.state); err != nil {
		return nil, fmt.Errorf("parse store %s: %w", path, err)
	}
	return s, nil
}

// Path returns the backing file path.
func (s *Store) Path() string { return s.path }

// Snapshot returns a deep copy of the current state.
func (s *Store) Snapshot() model.State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state.Clone()
}

// IsEmpty reports whether the store has no alerts, reports or myths.
func (s *Store) IsEmpty() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.state.Alerts) == 0 && len(s.state.Reports) == 0 && len(s.state.Myths) == 0
}

// Seed replaces an empty store with the supplied state.
func (s *Store) Seed(state model.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.state.Alerts) != 0 || len(s.state.Reports) != 0 || len(s.state.Myths) != 0 {
		return errors.New("refusing to seed a non-empty store")
	}
	s.state = state.Clone()
	return s.saveLocked()
}

// Alerts returns every alert, sorted by severity and recency.
func (s *Store) Alerts() []model.Alert {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Alert, len(s.state.Alerts))
	for i, alert := range s.state.Alerts {
		out[i] = alert.Clone()
	}
	model.SortAlerts(out)
	return out
}

// ActiveAlerts returns only active alerts.
func (s *Store) ActiveAlerts() []model.Alert {
	alerts := s.Alerts()
	out := alerts[:0]
	for _, alert := range alerts {
		if alert.Active {
			out = append(out, alert)
		}
	}
	return out
}

// Alert returns one alert by ID.
func (s *Store) Alert(id string) (model.Alert, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, alert := range s.state.Alerts {
		if alert.ID == id {
			return alert.Clone(), true
		}
	}
	return model.Alert{}, false
}

// UpsertAlert creates or replaces an alert.
func (s *Store) UpsertAlert(alert model.Alert) (model.Alert, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	alert.UpdatedAt = now
	if alert.ID == "" {
		alert.ID = model.NewID("alert")
	}
	if alert.CreatedAt.IsZero() {
		alert.CreatedAt = now
	}
	for i, existing := range s.state.Alerts {
		if existing.ID == alert.ID {
			alert.CreatedAt = existing.CreatedAt
			s.state.Alerts[i] = alert.Clone()
			return alert.Clone(), s.saveLocked()
		}
	}
	s.state.Alerts = append(s.state.Alerts, alert.Clone())
	return alert.Clone(), s.saveLocked()
}

// DeleteAlert removes an alert by ID.
func (s *Store) DeleteAlert(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, alert := range s.state.Alerts {
		if alert.ID == id {
			s.state.Alerts = append(s.state.Alerts[:i], s.state.Alerts[i+1:]...)
			return s.saveLocked()
		}
	}
	return ErrNotFound
}

// AddReport stores a new pending report.
func (s *Store) AddReport(report model.Report) (model.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if report.ID == "" {
		report.ID = model.NewID("report")
	}
	if report.CreatedAt.IsZero() {
		report.CreatedAt = time.Now().UTC()
	}
	if report.Status == "" {
		report.Status = model.ReportPending
	}
	s.state.Reports = append(s.state.Reports, report.Clone())
	return report.Clone(), s.saveLocked()
}

// Reports returns reports filtered by status. An empty status returns all.
func (s *Store) Reports(status model.ReportStatus) []model.Report {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Report, 0, len(s.state.Reports))
	for _, report := range s.state.Reports {
		if status == "" || report.Status == status {
			out = append(out, report.Clone())
		}
	}
	model.SortReports(out)
	return out
}

// Report returns one report by ID.
func (s *Store) Report(id string) (model.Report, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, report := range s.state.Reports {
		if report.ID == id {
			return report.Clone(), true
		}
	}
	return model.Report{}, false
}

// SetReportStatus moderates a report.
func (s *Store) SetReportStatus(id string, status model.ReportStatus, verified bool) (model.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status != model.ReportApproved && status != model.ReportRejected && status != model.ReportPending {
		return model.Report{}, fmt.Errorf("invalid report status %q", status)
	}
	for i, report := range s.state.Reports {
		if report.ID != id {
			continue
		}
		now := time.Now().UTC()
		report.Status = status
		report.Verified = verified
		report.ReviewedAt = &now
		s.state.Reports[i] = report
		return report.Clone(), s.saveLocked()
	}
	return model.Report{}, ErrNotFound
}

// Myths returns all myth-busting entries.
func (s *Store) Myths() []model.Myth {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Myth, len(s.state.Myths))
	for i, myth := range s.state.Myths {
		out[i] = myth.Clone()
	}
	return out
}

// UpsertMyth creates or replaces a myth-busting entry.
func (s *Store) UpsertMyth(myth model.Myth) (model.Myth, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	myth.UpdatedAt = time.Now().UTC()
	if myth.ID == "" {
		myth.ID = model.NewID("myth")
	}
	for i, existing := range s.state.Myths {
		if existing.ID == myth.ID {
			s.state.Myths[i] = myth
			return myth, s.saveLocked()
		}
	}
	s.state.Myths = append(s.state.Myths, myth)
	return myth, s.saveLocked()
}

// Replace swaps in a complete state. It is used by tooling that manages a
// snapshot state file directly.
func (s *Store) Replace(state model.State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state.Clone()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	s.state.Version++
	s.state.UpdatedAt = time.Now().UTC()

	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode store: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(s.path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create store directory: %w", err)
		}
	}

	tmp, err := os.CreateTemp(dir, ".state-*.json")
	if err != nil {
		return fmt.Errorf("create temporary store: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("set store permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write store: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync store: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close store: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace store: %w", err)
	}
	return nil
}
