package snapshot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"emergency-alert-system/internal/model"
)

func testState() model.State {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	return model.State{
		Version: 7,
		Alerts: []model.Alert{
			{
				ID:        "alert-active",
				Title:     "Flood warning for Riverside",
				Severity:  model.SeverityCritical,
				Areas:     []string{"Riverside"},
				Summary:   "Move to higher ground.",
				Guidance:  []string{"Avoid flood water."},
				Source:    "Test Agency",
				Official:  true,
				Active:    true,
				CreatedAt: now.Add(-time.Hour),
				UpdatedAt: now.Add(-time.Minute),
			},
			{
				ID:        "alert-inactive",
				Title:     "Resolved road closure",
				Severity:  model.SeverityInfo,
				Summary:   "The road has reopened.",
				Source:    "Test Highways",
				Active:    false,
				CreatedAt: now.Add(-48 * time.Hour),
				UpdatedAt: now.Add(-24 * time.Hour),
			},
		},
		Myths: []model.Myth{{
			ID:         "myth-1",
			Claim:      "The whole city is being evacuated.",
			Correction: "No city-wide evacuation has been ordered.",
			Source:     "Test Agency",
			UpdatedAt:  now,
		}},
	}
}

func TestWriteCreatesSelfContainedSnapshot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "snapshot")
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

	result, err := Write(dir, testState(), Options{Demo: true, Now: now})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if result.ActiveAlerts != 1 || result.Version != 7 || !result.GeneratedAt.Equal(now) {
		t.Fatalf("unexpected result: %+v", result)
	}

	expected := []string{
		"index.html",
		"site.css",
		"manifest.json",
		"snapshot.json",
		"govuk/govuk-frontend.min.css",
		"fonts/bold-b542beb274-v2.woff2",
		"images/favicon.ico",
		"data/alerts.json",
		"data/myths.json",
	}
	for _, name := range expected {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s: %v", name, err)
		}
	}

	index, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	html := string(index)
	for _, want := range []string{
		"Emergency information snapshot",
		"Flood warning for Riverside",
		"noindex,nofollow",
		"Read-only demonstration snapshot",
		"Snapshot version 7",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html missing %q", want)
		}
	}
	if strings.Contains(html, "Resolved road closure") {
		t.Error("inactive alert leaked into the snapshot")
	}
	if strings.Contains(html, "<script") {
		t.Error("snapshot should not contain scripts")
	}
	if strings.Contains(html, "/assets/") {
		t.Error("snapshot should use relative asset paths")
	}

	css, err := os.ReadFile(filepath.Join(dir, "govuk", "govuk-frontend.min.css"))
	if err != nil {
		t.Fatalf("read css: %v", err)
	}
	if strings.Contains(string(css), "/assets/") {
		t.Error("CSS still contains absolute /assets/ URLs")
	}
	if !strings.Contains(string(css), "url(../fonts/") {
		t.Error("CSS font URLs were not rewritten")
	}
	if !strings.Contains(string(css), "url(../images/") {
		t.Error("CSS image URLs were not rewritten")
	}

	var alerts alertsFile
	data, err := os.ReadFile(filepath.Join(dir, "data", "alerts.json"))
	if err != nil {
		t.Fatalf("read alerts data: %v", err)
	}
	if err := json.Unmarshal(data, &alerts); err != nil {
		t.Fatalf("parse alerts data: %v", err)
	}
	if len(alerts.Alerts) != 1 || alerts.Alerts[0].ID != "alert-active" {
		t.Fatalf("unexpected alerts data: %+v", alerts.Alerts)
	}
	if alerts.Version != 7 || !alerts.Demo || !alerts.GeneratedAt.Equal(now) {
		t.Fatalf("unexpected alerts metadata: %+v", alerts)
	}

	var myths mythsFile
	data, err = os.ReadFile(filepath.Join(dir, "data", "myths.json"))
	if err != nil {
		t.Fatalf("read myths data: %v", err)
	}
	if err := json.Unmarshal(data, &myths); err != nil {
		t.Fatalf("parse myths data: %v", err)
	}
	if len(myths.Myths) != 1 || myths.Myths[0].ID != "myth-1" {
		t.Fatalf("unexpected myths data: %+v", myths.Myths)
	}

	var meta metadata
	data, err = os.ReadFile(filepath.Join(dir, "snapshot.json"))
	if err != nil {
		t.Fatalf("read metadata: %v", err)
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("parse metadata: %v", err)
	}
	if meta.ActiveAlerts != 1 || meta.Version != 7 || !meta.Demo || !meta.GeneratedAt.Equal(now) {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
}

func TestWriteEmptyState(t *testing.T) {
	dir := t.TempDir()
	if _, err := Write(dir, model.State{Version: 1}, Options{}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	index, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if !strings.Contains(string(index), "There are no active alerts in this snapshot.") {
		t.Error("empty snapshot does not explain that there are no alerts")
	}
	if strings.Contains(string(index), "noindex,nofollow") {
		t.Error("non-demo snapshot should not be marked noindex")
	}
}

func TestWriteRequiresDirectory(t *testing.T) {
	if _, err := Write("", model.State{}, Options{}); err == nil {
		t.Fatal("expected an error for an empty directory")
	}
}
