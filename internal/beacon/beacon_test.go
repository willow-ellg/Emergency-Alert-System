package beacon

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"emergency-alert-system/internal/model"
	"emergency-alert-system/internal/seed"
	"emergency-alert-system/internal/snapshot"
)

func writeSnapshot(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "snapshot")
	_, err := snapshot.Write(dir, seed.Demo(), snapshot.Options{
		Demo: true,
		Now:  time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
	return dir
}

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return response, string(body)
}

func TestServerServesSnapshot(t *testing.T) {
	dir := writeSnapshot(t)
	server, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write hidden file: %v", err)
	}

	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	response, body := get(t, httpServer.URL+"/api/health")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", response.StatusCode)
	}
	var health map[string]any
	if err := json.Unmarshal([]byte(body), &health); err != nil {
		t.Fatalf("parse health: %v", err)
	}
	if health["status"] != "ok" || health["mode"] != "beacon" || health["alerts"] != float64(3) {
		t.Fatalf("unexpected health: %v", health)
	}

	response, body = get(t, httpServer.URL+"/")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("index status = %d", response.StatusCode)
	}
	if !strings.Contains(body, "Emergency information snapshot") {
		t.Error("index does not contain the snapshot heading")
	}
	if strings.Contains(body, "<script") {
		t.Error("snapshot should not contain scripts")
	}
	if got := response.Header.Get("Content-Security-Policy"); !strings.Contains(got, "script-src 'none'") {
		t.Fatalf("unexpected CSP: %q", got)
	}

	response, body = get(t, httpServer.URL+"/api/alerts")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("alerts status = %d", response.StatusCode)
	}
	var alerts []model.Alert
	if err := json.Unmarshal([]byte(body), &alerts); err != nil {
		t.Fatalf("parse alerts: %v", err)
	}
	if len(alerts) != 3 {
		t.Fatalf("alerts = %d, want 3", len(alerts))
	}

	response, _ = get(t, httpServer.URL+"/govuk/govuk-frontend.min.css")
	if response.StatusCode != http.StatusOK || !strings.HasPrefix(response.Header.Get("Content-Type"), "text/css") {
		t.Fatalf("css response: status=%d type=%q", response.StatusCode, response.Header.Get("Content-Type"))
	}

	for _, path := range []string{"/data/alerts.json", "/snapshot.json", "/site.css", "/images/favicon.ico"} {
		response, _ = get(t, httpServer.URL+path)
		if response.StatusCode != http.StatusOK {
			t.Errorf("GET %s status = %d", path, response.StatusCode)
		}
	}

	for _, path := range []string{"/nope", "/.hidden", "/data/"} {
		response, _ = get(t, httpServer.URL+path)
		if response.StatusCode != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, response.StatusCode)
		}
	}
}

func TestServerNotReady(t *testing.T) {
	server, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	httpServer := httptest.NewServer(server.Handler())
	defer httpServer.Close()

	response, body := get(t, httpServer.URL+"/")
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("index status = %d, want 503", response.StatusCode)
	}
	if !strings.Contains(body, "not ready") {
		t.Fatalf("unexpected not-ready body: %q", body)
	}

	response, _ = get(t, httpServer.URL+"/api/health")
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("health status = %d, want 503", response.StatusCode)
	}
}

func TestNewRequiresExistingDirectory(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected an error for a missing snapshot directory")
	}
	if _, err := New(""); err == nil {
		t.Fatal("expected an error for an empty directory")
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	dir := writeSnapshot(t)

	state, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(state.Alerts) != 3 {
		t.Fatalf("loaded alerts = %d, want 3", len(state.Alerts))
	}
	if !state.Demo {
		t.Fatal("demo flag was not preserved")
	}

	state.Alerts = append(state.Alerts, model.Alert{
		ID:        "alert-manual",
		Title:     "Manually added alert",
		Severity:  model.SeverityWarning,
		Summary:   "Added directly to the thin snapshot.",
		Source:    "Beacon operator",
		Active:    true,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	})
	state.Version++

	if _, err := Save(dir, state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.Alerts) != 4 {
		t.Fatalf("reloaded alerts = %d, want 4", len(reloaded.Alerts))
	}

	index, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if !strings.Contains(string(index), "Manually added alert") {
		t.Error("re-rendered index does not contain the new alert")
	}
}

func TestLoadMissingSnapshot(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("expected an error for a directory without snapshot data")
	}
}
