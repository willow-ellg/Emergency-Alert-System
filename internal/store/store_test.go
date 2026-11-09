package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"emergency-alert-system/internal/model"
	"emergency-alert-system/internal/seed"
)

func TestSeedAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	st, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !st.IsEmpty() {
		t.Fatal("new store should be empty")
	}
	if err := st.Seed(seed.Demo()); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if err := st.Seed(seed.Demo()); err == nil {
		t.Fatal("seeding a non-empty store should fail")
	}

	reloaded, err := New(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.Alerts()) != 4 {
		t.Fatalf("reloaded alerts = %d, want 4", len(reloaded.Alerts()))
	}
	if len(reloaded.Reports("")) != 3 {
		t.Fatalf("reloaded reports = %d, want 3", len(reloaded.Reports("")))
	}
	if len(reloaded.Myths()) != 3 {
		t.Fatalf("reloaded myths = %d, want 3", len(reloaded.Myths()))
	}
}

func TestUpsertAndDeleteAlert(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	created, err := st.UpsertAlert(model.Alert{
		Title:    "Test alert",
		Severity: model.SeverityWarning,
		Summary:  "Summary",
		Source:   "Test",
		Active:   true,
	})
	if err != nil {
		t.Fatalf("UpsertAlert: %v", err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatalf("created alert missing generated fields: %+v", created)
	}

	updated, err := st.UpsertAlert(model.Alert{
		ID:       created.ID,
		Title:    "Updated alert",
		Severity: model.SeverityCritical,
		Summary:  "Updated summary",
		Source:   "Test",
		Active:   true,
	})
	if err != nil {
		t.Fatalf("update alert: %v", err)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Fatal("update changed CreatedAt")
	}
	if updated.Title != "Updated alert" {
		t.Fatalf("updated title = %q", updated.Title)
	}

	if err := st.DeleteAlert(created.ID); err != nil {
		t.Fatalf("DeleteAlert: %v", err)
	}
	if _, ok := st.Alert(created.ID); ok {
		t.Fatal("deleted alert is still present")
	}
	if err := st.DeleteAlert(created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete error = %v, want ErrNotFound", err)
	}
}

func TestReportModeration(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	report, err := st.AddReport(model.Report{
		Area:     "Riverside",
		Category: "road",
		Body:     "Bridge closed",
	})
	if err != nil {
		t.Fatalf("AddReport: %v", err)
	}
	if report.ID == "" || report.Status != model.ReportPending || report.CreatedAt.IsZero() {
		t.Fatalf("created report missing generated fields: %+v", report)
	}
	if got := len(st.Reports(model.ReportPending)); got != 1 {
		t.Fatalf("pending reports = %d, want 1", got)
	}
	if got := len(st.Reports(model.ReportApproved)); got != 0 {
		t.Fatalf("approved reports = %d, want 0", got)
	}

	approved, err := st.SetReportStatus(report.ID, model.ReportApproved, true)
	if err != nil {
		t.Fatalf("SetReportStatus: %v", err)
	}
	if approved.Status != model.ReportApproved || !approved.Verified || approved.ReviewedAt == nil {
		t.Fatalf("approved report fields: %+v", approved)
	}
	if got := len(st.Reports(model.ReportApproved)); got != 1 {
		t.Fatalf("approved reports = %d, want 1", got)
	}
	if _, err := st.SetReportStatus("missing", model.ReportApproved, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing report error = %v, want ErrNotFound", err)
	}
}

func TestReturnedValuesAreClones(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := st.Seed(model.State{
		Alerts: []model.Alert{{
			ID:       "alert-1",
			Title:    "Original",
			Severity: model.SeverityInfo,
			Areas:    []string{"Riverside"},
			Guidance: []string{"Stay safe"},
			Active:   true,
		}},
	}); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	alerts := st.Alerts()
	alerts[0].Title = "Mutated"
	alerts[0].Areas[0] = "Mutated"
	alerts[0].Guidance[0] = "Mutated"

	stored, ok := st.Alert("alert-1")
	if !ok {
		t.Fatal("alert not found")
	}
	if stored.Title != "Original" || stored.Areas[0] != "Riverside" || stored.Guidance[0] != "Stay safe" {
		t.Fatalf("store state was mutated through a returned value: %+v", stored)
	}
}

func TestReplace(t *testing.T) {
	st, err := New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := st.Replace(model.State{
		Alerts: []model.Alert{{
			ID:        "alert-1",
			Title:     "Replacement",
			Severity:  model.SeverityInfo,
			Active:    true,
			CreatedAt: time.Now().UTC(),
		}},
	}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if got := len(st.Alerts()); got != 1 {
		t.Fatalf("alerts after replace = %d, want 1", got)
	}
	if st.Snapshot().Version == 0 {
		t.Fatal("version was not incremented")
	}
}
