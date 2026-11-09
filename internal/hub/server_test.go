package hub

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"emergency-alert-system/internal/model"
	"emergency-alert-system/internal/seed"
	"emergency-alert-system/internal/store"
)

func newTestHub(t *testing.T, demo bool) (*Server, *store.Store, string) {
	t.Helper()
	root := t.TempDir()
	st, err := store.New(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	if err := st.Seed(seed.Demo()); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	snapshotDir := filepath.Join(root, "snapshot")
	server, err := New(st, Options{
		AdminToken:  "test-token",
		SnapshotDir: snapshotDir,
		Demo:        demo,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return server, st, snapshotDir
}

func request(t *testing.T, server *Server, method, target string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)
	return recorder
}

func adminHeaders() map[string]string {
	return map[string]string{"Authorization": "Bearer test-token"}
}

func TestIndexRendersPublicPageWithSecurityHeaders(t *testing.T) {
	server, _, _ := newTestHub(t, true)
	recorder := request(t, server, http.MethodGet, "/", nil, nil)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{
		"Emergency Response Hub",
		"Flood warning: Riverside and Lowbridge",
		"Power outage: Northgate",
		"National alert system test",
		"app-alert-card",
		"DEMONSTRATION DATA",
		"999",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}
	if strings.Contains(body, "alert-demo-closed") {
		t.Error("inactive alert leaked into the public page")
	}

	csp := recorder.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "frame-ancestors 'none'", "object-src 'none'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q: %q", want, csp)
		}
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
}

func TestAlertsAPIFilters(t *testing.T) {
	server, _, _ := newTestHub(t, true)

	tests := []struct {
		target string
		want   int
	}{
		{"/api/alerts", 3},
		{"/api/alerts?area=Riverside", 2},
		{"/api/alerts?area=Northgate", 2},
		{"/api/alerts?lat=51.501&lon=-0.100", 2},
		{"/api/alerts?lat=55.0&lon=-3.0", 1},
	}
	for _, test := range tests {
		recorder := request(t, server, http.MethodGet, test.target, nil, nil)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d", test.target, recorder.Code)
		}
		var alerts []model.Alert
		if err := json.Unmarshal(recorder.Body.Bytes(), &alerts); err != nil {
			t.Fatalf("parse %s: %v", test.target, err)
		}
		if len(alerts) != test.want {
			t.Errorf("GET %s returned %d alerts, want %d", test.target, len(alerts), test.want)
		}
	}
}

func TestReportSubmissionAndModeration(t *testing.T) {
	server, _, _ := newTestHub(t, true)

	payload := `{"area":"Riverside","category":"road","body":"Bridge closed near the mill."}`
	recorder := request(t, server, http.MethodPost, "/api/reports", strings.NewReader(payload), map[string]string{
		"Content-Type": "application/json",
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create report status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var created struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse created report: %v", err)
	}
	if created.ID == "" || created.Status != "pending" {
		t.Fatalf("unexpected created report: %+v", created)
	}

	recorder = request(t, server, http.MethodGet, "/api/admin/reports?status=pending", nil, nil)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("admin without token status = %d, want 401", recorder.Code)
	}
	recorder = request(t, server, http.MethodGet, "/api/admin/reports?status=pending", nil, map[string]string{
		"Authorization": "Bearer wrong-token",
	})
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("admin with wrong token status = %d, want 401", recorder.Code)
	}

	recorder = request(t, server, http.MethodGet, "/api/admin/reports?status=pending", nil, adminHeaders())
	if recorder.Code != http.StatusOK {
		t.Fatalf("admin pending status = %d", recorder.Code)
	}
	var pending []model.Report
	if err := json.Unmarshal(recorder.Body.Bytes(), &pending); err != nil {
		t.Fatalf("parse pending reports: %v", err)
	}
	foundPending := false
	for _, report := range pending {
		if report.ID == created.ID {
			foundPending = true
		}
	}
	if !foundPending {
		t.Fatalf("submitted report was not queued for moderation: %+v", pending)
	}

	approveURL := "/api/admin/reports/" + url.PathEscape(created.ID) + "/approve"
	recorder = request(t, server, http.MethodPost, approveURL, strings.NewReader(`{"verified":true}`), adminHeaders())
	if recorder.Code != http.StatusOK {
		t.Fatalf("approve status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var approved model.Report
	if err := json.Unmarshal(recorder.Body.Bytes(), &approved); err != nil {
		t.Fatalf("parse approved report: %v", err)
	}
	if approved.Status != model.ReportApproved || !approved.Verified {
		t.Fatalf("unexpected approved report: %+v", approved)
	}

	recorder = request(t, server, http.MethodGet, "/api/reports?area=Riverside", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("public reports status = %d", recorder.Code)
	}
	var published []model.Report
	if err := json.Unmarshal(recorder.Body.Bytes(), &published); err != nil {
		t.Fatalf("parse public reports: %v", err)
	}
	found := false
	for _, report := range published {
		if report.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("approved report was not published")
	}
}

func TestReportFormRedirectsWithoutJavaScript(t *testing.T) {
	server, _, _ := newTestHub(t, true)
	form := url.Values{
		"area":     {"Riverside"},
		"category": {"road"},
		"body":     {"Bridge closed."},
	}
	recorder := request(t, server, http.MethodPost, "/reports", strings.NewReader(form.Encode()), map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	})
	if recorder.Code != http.StatusSeeOther {
		t.Fatalf("form status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	location := recorder.Header().Get("Location")
	if !strings.Contains(location, "submitted=1") || !strings.Contains(location, "area=Riverside") {
		t.Fatalf("unexpected redirect: %q", location)
	}

	recorder = request(t, server, http.MethodGet, location, nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("follow-up status = %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "Your report has been sent for checking.") {
		t.Error("success banner was not rendered")
	}
}

func TestReportRateLimit(t *testing.T) {
	server, _, _ := newTestHub(t, true)
	payload := `{"area":"Riverside","category":"other","body":"Repeated update."}`

	for i := 0; i < 6; i++ {
		recorder := request(t, server, http.MethodPost, "/api/reports", strings.NewReader(payload), map[string]string{
			"Content-Type": "application/json",
		})
		if recorder.Code != http.StatusCreated {
			t.Fatalf("request %d status = %d, want 201", i+1, recorder.Code)
		}
	}

	recorder := request(t, server, http.MethodPost, "/api/reports", strings.NewReader(payload), map[string]string{
		"Content-Type": "application/json",
	})
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited status = %d, want 429", recorder.Code)
	}
	if got := recorder.Header().Get("Retry-After"); got != "600" {
		t.Fatalf("Retry-After = %q", got)
	}
}

func TestAdminAlertLifecycle(t *testing.T) {
	server, _, _ := newTestHub(t, true)

	payload := `{
		"title": "Test operator alert",
		"severity": "critical",
		"summary": "This is an operator-created test alert.",
		"guidance": ["Act now", "Call 999 if at risk"],
		"areas": ["Testville"],
		"source": "Test operator",
		"official": true,
		"active": true
	}`
	recorder := request(t, server, http.MethodPost, "/api/admin/alerts", strings.NewReader(payload), map[string]string{
		"Authorization": "Bearer test-token",
		"Content-Type":  "application/json",
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create alert status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var created model.Alert
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("parse created alert: %v", err)
	}
	if created.ID == "" || created.Title != "Test operator alert" {
		t.Fatalf("unexpected created alert: %+v", created)
	}

	recorder = request(t, server, http.MethodGet, "/api/admin/alerts", nil, adminHeaders())
	if recorder.Code != http.StatusOK {
		t.Fatalf("admin alerts status = %d", recorder.Code)
	}
	var all []model.Alert
	if err := json.Unmarshal(recorder.Body.Bytes(), &all); err != nil {
		t.Fatalf("parse admin alerts: %v", err)
	}
	if len(all) != 5 {
		t.Fatalf("admin alerts = %d, want 5", len(all))
	}

	recorder = request(t, server, http.MethodGet, "/api/alerts/"+url.PathEscape(created.ID), nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("public alert status = %d", recorder.Code)
	}

	recorder = request(t, server, http.MethodDelete, "/api/admin/alerts/"+url.PathEscape(created.ID), nil, adminHeaders())
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("delete alert status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	recorder = request(t, server, http.MethodGet, "/api/alerts/"+url.PathEscape(created.ID), nil, nil)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("deleted alert status = %d, want 404", recorder.Code)
	}
}

func TestSnapshotEndpoint(t *testing.T) {
	server, _, snapshotDir := newTestHub(t, true)
	recorder := request(t, server, http.MethodPost, "/api/admin/snapshot", nil, adminHeaders())
	if recorder.Code != http.StatusOK {
		t.Fatalf("snapshot status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var result struct {
		Directory string `json:"directory"`
		Alerts    int    `json:"alerts"`
		Version   int    `json:"version"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse snapshot result: %v", err)
	}
	if result.Directory != snapshotDir || result.Alerts != 3 {
		t.Fatalf("unexpected snapshot result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, "index.html")); err != nil {
		t.Fatalf("snapshot index: %v", err)
	}
	if _, err := os.Stat(filepath.Join(snapshotDir, "data", "alerts.json")); err != nil {
		t.Fatalf("snapshot alerts data: %v", err)
	}
}

func TestAdminDisabledWithoutToken(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	server, err := New(st, Options{SnapshotDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	recorder := request(t, server, http.MethodGet, "/api/admin/alerts", nil, nil)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func TestHealthAndStaticAssets(t *testing.T) {
	server, _, _ := newTestHub(t, true)

	recorder := request(t, server, http.MethodGet, "/api/health", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("health status = %d", recorder.Code)
	}
	var health map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &health); err != nil {
		t.Fatalf("parse health: %v", err)
	}
	if health["mode"] != "hub" || health["alerts"] != float64(4) {
		t.Fatalf("unexpected health: %v", health)
	}

	recorder = request(t, server, http.MethodGet, "/assets/govuk/govuk-frontend.min.css", nil, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("css status = %d", recorder.Code)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Fatalf("css content type = %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "public, max-age=3600" {
		t.Fatalf("asset cache control = %q", got)
	}
}

func TestBrokerBroadcast(t *testing.T) {
	b := newBroker()
	messages, unsubscribe := b.Subscribe()
	defer unsubscribe()

	b.Broadcast("update", map[string]any{"alert_id": "alert-1"})
	select {
	case message := <-messages:
		if !strings.Contains(message, "event: update") || !strings.Contains(message, "alert-1") {
			t.Fatalf("unexpected broker message: %q", message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for broker message")
	}
}

func TestRateLimiterRefills(t *testing.T) {
	limiter := newRateLimiter(1, time.Minute)
	if !limiter.Allow("client") {
		t.Fatal("first request should be allowed")
	}
	if limiter.Allow("client") {
		t.Fatal("second request should be limited")
	}
	if !limiter.Allow("other-client") {
		t.Fatal("a different client should be allowed")
	}
}

func TestClientIPTrustProxy(t *testing.T) {
	server := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.10:1234"

	if got := server.clientIP(req); got != "192.0.2.10" {
		t.Fatalf("direct client IP = %q", got)
	}

	server.trustProxy = true
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	if got := server.clientIP(req); got != "203.0.113.9" {
		t.Fatalf("forwarded client IP = %q", got)
	}

	req.Header.Set("X-Forwarded-For", "not-an-ip")
	req.Header.Set("X-Real-IP", "198.51.100.4")
	if got := server.clientIP(req); got != "198.51.100.4" {
		t.Fatalf("real client IP = %q", got)
	}
}

func TestDecodeJSONRejectsUnknownFields(t *testing.T) {
	server, _, _ := newTestHub(t, true)
	recorder := request(t, server, http.MethodPost, "/api/admin/alerts", bytes.NewBufferString(`{"title":"x","unknown":true}`), map[string]string{
		"Authorization": "Bearer test-token",
		"Content-Type":  "application/json",
	})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}
