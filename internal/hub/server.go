// Package hub implements the full, connected emergency response hub.
//
// The hub is intentionally separate from the thin snapshot system: it owns the
// database, moderation and live update stream. The thin system never depends on
// it at runtime.
package hub

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	urlpath "path"
	"strconv"
	"strings"
	"sync"
	"time"

	"emergency-alert-system/internal/model"
	"emergency-alert-system/internal/snapshot"
	"emergency-alert-system/internal/store"
	"emergency-alert-system/internal/view"
	"emergency-alert-system/web"
)

// Options configures a hub server.
type Options struct {
	AdminToken  string
	SnapshotDir string
	Demo        bool
	// TrustProxy makes the server read X-Forwarded-For / X-Real-IP for rate
	// limiting. Only enable it when a trusted reverse proxy sets those headers.
	TrustProxy bool
}

// Server is the full emergency response hub.
type Server struct {
	store       *store.Store
	adminToken  string
	snapshotDir string
	demo        bool
	trustProxy  bool
	now         func() time.Time

	broker *broker
	limit  *rateLimiter

	page           *template.Template
	adminPage      *template.Template
	alertsPartial  *template.Template
	reportsPartial *template.Template
	static         http.Handler
}

// AlertView adds request-specific distance information to an alert.
type AlertView struct {
	model.Alert
	DistanceKM  float64
	HasDistance bool
}

// Query is the parsed public location filter.
type Query struct {
	Area        string
	Lat         string
	Lon         string
	HasLocation bool
	LatValue    float64
	LonValue    float64
}

// PageData is the template model for public and operator pages.
type PageData struct {
	Title       string
	AssetPrefix string
	HomeURL     string
	Static      bool
	ExtraScript string
	PhaseTag    string
	PhaseText   string
	Now         time.Time
	Demo        bool

	Alerts   []AlertView
	Reports  []model.Report
	Myths    []model.Myth
	Query    Query
	Filtered bool
	Top      *AlertView

	Submitted bool
	Error     string
}

// New builds a hub server and parses its embedded templates.
func New(st *store.Store, opts Options) (*Server, error) {
	staticFS, err := fs.Sub(web.FS, "static")
	if err != nil {
		return nil, fmt.Errorf("open static assets: %w", err)
	}

	funcs := view.FuncMap(time.Now)
	page, err := template.New("layout").Funcs(funcs).ParseFS(web.FS,
		"templates/layout.html",
		"templates/index.html",
		"templates/alerts.html",
		"templates/reports.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse public templates: %w", err)
	}

	adminPage, err := template.New("layout").Funcs(funcs).ParseFS(web.FS,
		"templates/layout.html",
		"templates/admin.html",
	)
	if err != nil {
		return nil, fmt.Errorf("parse admin templates: %w", err)
	}

	alertsPartial, err := template.New("alerts").Funcs(funcs).ParseFS(web.FS, "templates/alerts.html")
	if err != nil {
		return nil, fmt.Errorf("parse alerts partial: %w", err)
	}

	reportsPartial, err := template.New("reports").Funcs(funcs).ParseFS(web.FS, "templates/reports.html")
	if err != nil {
		return nil, fmt.Errorf("parse reports partial: %w", err)
	}

	return &Server{
		store:          st,
		adminToken:     opts.AdminToken,
		snapshotDir:    opts.SnapshotDir,
		demo:           opts.Demo,
		trustProxy:     opts.TrustProxy,
		now:            time.Now,
		broker:         newBroker(),
		limit:          newRateLimiter(6, 10*time.Minute),
		page:           page,
		adminPage:      adminPage,
		alertsPartial:  alertsPartial,
		reportsPartial: reportsPartial,
		static:         http.StripPrefix("/assets/", http.FileServer(http.FS(staticFS))),
	}, nil
}

// Handler returns the complete HTTP handler, including security middleware.
func (s *Server) Handler() http.Handler {
	return s.securityHeaders(s.recoverer(s.routes()))
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /admin", s.handleAdmin)
	mux.HandleFunc("GET /assets/", s.handleStatic)

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/alerts", s.handleAlerts)
	mux.HandleFunc("GET /api/alerts/{id}", s.handleAlert)
	mux.HandleFunc("GET /api/reports", s.handleReports)
	mux.HandleFunc("POST /api/reports", s.handleCreateReport)
	mux.HandleFunc("GET /api/myths", s.handleMyths)
	mux.HandleFunc("GET /api/stream", s.handleStream)

	mux.HandleFunc("GET /partials/alerts", s.handleAlertsPartial)
	mux.HandleFunc("GET /partials/reports", s.handleReportsPartial)
	mux.HandleFunc("POST /reports", s.handleReportForm)

	mux.HandleFunc("GET /api/admin/alerts", s.requireAdmin(s.handleAdminAlerts))
	mux.HandleFunc("POST /api/admin/alerts", s.requireAdmin(s.handleAdminUpsertAlert))
	mux.HandleFunc("DELETE /api/admin/alerts/{id}", s.requireAdmin(s.handleAdminDeleteAlert))
	mux.HandleFunc("GET /api/admin/reports", s.requireAdmin(s.handleAdminReports))
	mux.HandleFunc("POST /api/admin/reports/{id}/approve", s.requireAdmin(s.handleAdminModerate(true)))
	mux.HandleFunc("POST /api/admin/reports/{id}/reject", s.requireAdmin(s.handleAdminModerate(false)))
	mux.HandleFunc("POST /api/admin/myths", s.requireAdmin(s.handleAdminUpsertMyth))
	mux.HandleFunc("POST /api/admin/snapshot", s.requireAdmin(s.handleAdminSnapshot))

	return mux
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; object-src 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "geolocation=(self), camera=(), microphone=(), payment=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, recovered)
				http.Error(w, "Something went wrong. Please try again.", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data := s.buildPageData(r)
	data.Submitted = r.URL.Query().Get("submitted") == "1"
	data.Error = r.URL.Query().Get("error")
	s.render(w, s.page, "layout", data)
}

func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	data := PageData{
		Title:       "Operator area",
		AssetPrefix: "/assets/",
		HomeURL:     "/",
		ExtraScript: "admin.js",
		PhaseTag:    "Operators",
		PhaseText:   "This area requires an operator token. It is not part of the public service.",
		Now:         s.now(),
		Demo:        true,
	}
	s.render(w, s.adminPage, "layout", data)
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/") {
		http.NotFound(w, r)
		return
	}
	setContentType(w, r.URL.Path)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	s.static.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	state := s.store.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"mode":    "hub",
		"alerts":  len(state.Alerts),
		"reports": len(state.Reports),
		"version": state.Version,
		"time":    s.now().UTC(),
	})
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	q := parseQuery(r)
	alerts := s.store.ActiveAlerts()
	out := make([]model.Alert, 0, len(alerts))
	for _, alert := range alerts {
		if alert.Matches(q.LatValue, q.LonValue, q.HasLocation, q.Area) {
			out = append(out, alert)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAlert(w http.ResponseWriter, r *http.Request) {
	alert, ok := s.store.Alert(r.PathValue("id"))
	if !ok || !alert.Active {
		writeError(w, http.StatusNotFound, "alert not found")
		return
	}
	writeJSON(w, http.StatusOK, alert)
}

func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	area := strings.TrimSpace(r.URL.Query().Get("area"))
	alertID := strings.TrimSpace(r.URL.Query().Get("alert_id"))
	reports := s.store.Reports(model.ReportApproved)
	out := make([]model.Report, 0, len(reports))
	for _, report := range reports {
		if alertID != "" && report.AlertID != alertID {
			continue
		}
		if area != "" && !model.AreaMatches([]string{report.Area}, area) {
			continue
		}
		out = append(out, report)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleMyths(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Myths())
}

func (s *Server) handleAlertsPartial(w http.ResponseWriter, r *http.Request) {
	s.render(w, s.alertsPartial, "alerts", s.buildPageData(r))
}

func (s *Server) handleReportsPartial(w http.ResponseWriter, r *http.Request) {
	s.render(w, s.reportsPartial, "reports", s.buildPageData(r))
}

func (s *Server) handleCreateReport(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
		writeError(w, http.StatusUnsupportedMediaType, "content type must be application/json")
		return
	}

	var req reportInput
	if err := decodeJSON(w, r, &req, 16<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	report, err := s.validateReport(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if !s.limit.Allow(s.clientIP(r)) {
		w.Header().Set("Retry-After", "600")
		writeError(w, http.StatusTooManyRequests, "too many reports from this address; try again later")
		return
	}

	created, err := s.store.AddReport(report)
	if err != nil {
		log.Printf("add report: %v", err)
		writeError(w, http.StatusInternalServerError, "the report could not be saved")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":      created.ID,
		"status":  created.Status,
		"message": "report received and queued for moderation",
	})
}

func (s *Server) handleReportForm(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		redirectError(w, r, "The report could not be read. Please try again.")
		return
	}

	lat, _ := strconv.ParseFloat(r.PostFormValue("lat"), 64)
	lon, _ := strconv.ParseFloat(r.PostFormValue("lon"), 64)
	report, err := s.validateReport(reportInput{
		Area:     r.PostFormValue("area"),
		Category: r.PostFormValue("category"),
		AlertID:  r.PostFormValue("alert_id"),
		Body:     r.PostFormValue("body"),
		Lat:      lat,
		Lon:      lon,
	})
	if err != nil {
		redirectError(w, r, err.Error())
		return
	}

	if !s.limit.Allow(s.clientIP(r)) {
		redirectError(w, r, "Too many reports from this address. Please try again later.")
		return
	}

	if _, err := s.store.AddReport(report); err != nil {
		log.Printf("add report form: %v", err)
		redirectError(w, r, "The report could not be saved. Please try again.")
		return
	}

	values := url.Values{"submitted": {"1"}}
	if area := strings.TrimSpace(report.Area); area != "" {
		values.Set("area", area)
	}
	http.Redirect(w, r, "/?"+values.Encode(), http.StatusSeeOther)
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	messages, unsubscribe := s.broker.Subscribe()
	defer unsubscribe()

	if _, err := io.WriteString(w, "event: ready\ndata: {}\n\n"); err != nil {
		return
	}
	flusher.Flush()

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case message, open := <-messages:
			if !open {
				return
			}
			if _, err := io.WriteString(w, message); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := io.WriteString(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (s *Server) handleAdminAlerts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.Alerts())
}

func (s *Server) handleAdminUpsertAlert(w http.ResponseWriter, r *http.Request) {
	var req alertRequest
	if err := decodeJSON(w, r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	alert, err := validateAlert(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	created, err := s.store.UpsertAlert(alert)
	if err != nil {
		log.Printf("upsert alert: %v", err)
		writeError(w, http.StatusInternalServerError, "the alert could not be saved")
		return
	}

	s.broadcastUpdate()
	status := http.StatusOK
	if req.ID == "" {
		status = http.StatusCreated
	}
	writeJSON(w, status, created)
}

func (s *Server) handleAdminDeleteAlert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.store.DeleteAlert(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "alert not found")
			return
		}
		log.Printf("delete alert: %v", err)
		writeError(w, http.StatusInternalServerError, "the alert could not be deleted")
		return
	}
	s.broadcastUpdate()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAdminReports(w http.ResponseWriter, r *http.Request) {
	status := model.ReportStatus(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status"))))
	switch status {
	case "", "all":
		status = ""
	case model.ReportPending, model.ReportApproved, model.ReportRejected:
	default:
		writeError(w, http.StatusBadRequest, "status must be pending, approved, rejected or all")
		return
	}
	writeJSON(w, http.StatusOK, s.store.Reports(status))
}

func (s *Server) handleAdminModerate(approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		verified := approve
		var req struct {
			Verified *bool `json:"verified"`
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4<<10))
		if err != nil {
			writeError(w, http.StatusBadRequest, "could not read request body")
			return
		}
		if len(bytes.TrimSpace(body)) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
			if req.Verified != nil && approve {
				verified = *req.Verified
			}
		}

		status := model.ReportRejected
		if approve {
			status = model.ReportApproved
		}

		report, err := s.store.SetReportStatus(r.PathValue("id"), status, verified)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				writeError(w, http.StatusNotFound, "report not found")
				return
			}
			log.Printf("moderate report: %v", err)
			writeError(w, http.StatusInternalServerError, "the report could not be updated")
			return
		}

		s.broadcastUpdate()
		writeJSON(w, http.StatusOK, report)
	}
}

func (s *Server) handleAdminUpsertMyth(w http.ResponseWriter, r *http.Request) {
	var req mythRequest
	if err := decodeJSON(w, r, &req, 16<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	claim := strings.TrimSpace(req.Claim)
	correction := strings.TrimSpace(req.Correction)
	source := strings.TrimSpace(req.Source)
	if claim == "" || correction == "" {
		writeError(w, http.StatusBadRequest, "claim and correction are required")
		return
	}
	if len([]rune(claim)) > 300 || len([]rune(correction)) > 1000 {
		writeError(w, http.StatusBadRequest, "claim or correction is too long")
		return
	}
	if source == "" {
		source = "Emergency Response Hub"
	}

	myth, err := s.store.UpsertMyth(model.Myth{
		ID:         strings.TrimSpace(req.ID),
		Claim:      claim,
		Correction: correction,
		Source:     source,
	})
	if err != nil {
		log.Printf("upsert myth: %v", err)
		writeError(w, http.StatusInternalServerError, "the correction could not be saved")
		return
	}

	s.broadcastUpdate()
	status := http.StatusOK
	if req.ID == "" {
		status = http.StatusCreated
	}
	writeJSON(w, status, myth)
}

func (s *Server) handleAdminSnapshot(w http.ResponseWriter, r *http.Request) {
	if s.snapshotDir == "" {
		writeError(w, http.StatusServiceUnavailable, "snapshot directory is not configured")
		return
	}

	result, err := snapshot.Write(s.snapshotDir, s.store.Snapshot(), snapshot.Options{
		Demo: s.demo,
		Now:  s.now(),
	})
	if err != nil {
		log.Printf("write snapshot: %v", err)
		writeError(w, http.StatusInternalServerError, "the snapshot could not be generated")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"directory":    result.Directory,
		"alerts":       result.ActiveAlerts,
		"generated_at": result.GeneratedAt,
		"version":      result.Version,
	})
}

func (s *Server) buildPageData(r *http.Request) PageData {
	q := parseQuery(r)
	active := s.store.ActiveAlerts()

	views := make([]AlertView, 0, len(active))
	for _, alert := range active {
		if !alert.Matches(q.LatValue, q.LonValue, q.HasLocation, q.Area) {
			continue
		}
		view := AlertView{Alert: alert}
		if q.HasLocation && alert.HasGeo() {
			view.DistanceKM = model.DistanceKM(q.LatValue, q.LonValue, alert.Lat, alert.Lon)
			view.HasDistance = true
		}
		views = append(views, view)
	}

	reports := s.store.Reports(model.ReportApproved)
	if q.Area != "" {
		filtered := reports[:0]
		for _, report := range reports {
			if model.AreaMatches([]string{report.Area}, q.Area) {
				filtered = append(filtered, report)
			}
		}
		reports = filtered
	}

	var top *AlertView
	if len(views) > 0 {
		top = &views[0]
	}

	return PageData{
		Title:       "Emergency Response Hub",
		AssetPrefix: "/assets/",
		HomeURL:     "/",
		PhaseTag:    "Prototype",
		PhaseText:   "This is a demonstration service. For a real emergency call 999.",
		Now:         s.now(),
		Demo:        s.demo,
		Alerts:      views,
		Reports:     reports,
		Myths:       s.store.Myths(),
		Query:       q,
		Filtered:    q.Area != "" || q.HasLocation,
		Top:         top,
	}
}

func (s *Server) render(w http.ResponseWriter, tmpl *template.Template, name string, data any) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "Something went wrong. Please try again.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = buf.WriteTo(w)
}

func (s *Server) broadcastUpdate() {
	s.broker.Broadcast("update", map[string]any{"at": s.now().UTC()})
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.adminToken == "" {
			writeError(w, http.StatusServiceUnavailable, "operator access is disabled; set HUB_ADMIN_TOKEN")
			return
		}

		token := bearerToken(r)
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.adminToken)) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="emergency-hub-admin"`)
			writeError(w, http.StatusUnauthorized, "operator token is missing or invalid")
			return
		}

		next(w, r)
	}
}

type reportInput struct {
	Area     string  `json:"area"`
	Category string  `json:"category"`
	AlertID  string  `json:"alert_id"`
	Body     string  `json:"body"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
}

func (s *Server) validateReport(in reportInput) (model.Report, error) {
	area := strings.TrimSpace(in.Area)
	body := strings.TrimSpace(in.Body)
	category := strings.ToLower(strings.TrimSpace(in.Category))
	alertID := strings.TrimSpace(in.AlertID)

	if area == "" {
		return model.Report{}, errors.New("area is required")
	}
	if len([]rune(area)) > 120 {
		return model.Report{}, errors.New("area must be 120 characters or fewer")
	}
	if body == "" {
		return model.Report{}, errors.New("description is required")
	}
	if len([]rune(body)) > 1000 {
		return model.Report{}, errors.New("description must be 1000 characters or fewer")
	}
	if category == "" {
		category = "other"
	}
	if !allowedCategory(category) {
		return model.Report{}, errors.New("category must be hazard, road, shelter, medical, supplies, utility or other")
	}
	if alertID != "" {
		if _, ok := s.store.Alert(alertID); !ok {
			return model.Report{}, errors.New("related alert was not found")
		}
	}
	if (in.Lat != 0 || in.Lon != 0) && !validLatLon(in.Lat, in.Lon) {
		return model.Report{}, errors.New("latitude or longitude is invalid")
	}

	return model.Report{
		AlertID:  alertID,
		Area:     area,
		Category: category,
		Body:     body,
		Lat:      in.Lat,
		Lon:      in.Lon,
		Status:   model.ReportPending,
	}, nil
}

type alertRequest struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Severity string   `json:"severity"`
	Areas    []string `json:"areas"`
	Lat      float64  `json:"lat"`
	Lon      float64  `json:"lon"`
	RadiusKM float64  `json:"radius_km"`
	Summary  string   `json:"summary"`
	Guidance []string `json:"guidance"`
	Source   string   `json:"source"`
	Official *bool    `json:"official"`
	Active   *bool    `json:"active"`
}

func validateAlert(req alertRequest) (model.Alert, error) {
	title := strings.TrimSpace(req.Title)
	summary := strings.TrimSpace(req.Summary)
	source := strings.TrimSpace(req.Source)

	severity, err := model.ParseSeverity(req.Severity)
	if err != nil {
		return model.Alert{}, err
	}
	if title == "" {
		return model.Alert{}, errors.New("title is required")
	}
	if len([]rune(title)) > 160 {
		return model.Alert{}, errors.New("title must be 160 characters or fewer")
	}
	if summary == "" {
		return model.Alert{}, errors.New("summary is required")
	}
	if len([]rune(summary)) > 1000 {
		return model.Alert{}, errors.New("summary must be 1000 characters or fewer")
	}
	if source == "" {
		source = "Emergency Response Hub"
	}
	if len([]rune(source)) > 120 {
		return model.Alert{}, errors.New("source must be 120 characters or fewer")
	}

	areas, err := cleanList(req.Areas, 20, 80, "area")
	if err != nil {
		return model.Alert{}, err
	}
	guidance, err := cleanList(req.Guidance, 20, 300, "guidance step")
	if err != nil {
		return model.Alert{}, err
	}

	if req.RadiusKM < 0 {
		return model.Alert{}, errors.New("radius must not be negative")
	}
	if req.RadiusKM > 1000 {
		return model.Alert{}, errors.New("radius must be 1000 km or less")
	}
	if req.RadiusKM > 0 {
		if !validLatLon(req.Lat, req.Lon) {
			return model.Alert{}, errors.New("latitude or longitude is invalid")
		}
		if req.Lat == 0 && req.Lon == 0 {
			return model.Alert{}, errors.New("set latitude and longitude when a radius is used")
		}
	}

	official := true
	if req.Official != nil {
		official = *req.Official
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}

	return model.Alert{
		ID:       strings.TrimSpace(req.ID),
		Title:    title,
		Severity: severity,
		Areas:    areas,
		Lat:      req.Lat,
		Lon:      req.Lon,
		RadiusKM: req.RadiusKM,
		Summary:  summary,
		Guidance: guidance,
		Source:   source,
		Official: official,
		Active:   active,
	}, nil
}

type mythRequest struct {
	ID         string `json:"id"`
	Claim      string `json:"claim"`
	Correction string `json:"correction"`
	Source     string `json:"source"`
}

func cleanList(values []string, maxItems, maxRunes int, label string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if len([]rune(value)) > maxRunes {
			return nil, fmt.Errorf("each %s must be %d characters or fewer", label, maxRunes)
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	if len(out) > maxItems {
		return nil, fmt.Errorf("at most %d %ss are allowed", maxItems, label)
	}
	return out, nil
}

func allowedCategory(category string) bool {
	switch category {
	case "hazard", "road", "shelter", "medical", "supplies", "utility", "other":
		return true
	default:
		return false
	}
}

func validLatLon(lat, lon float64) bool {
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

func parseQuery(r *http.Request) Query {
	values := r.URL.Query()
	q := Query{
		Area: strings.TrimSpace(values.Get("area")),
		Lat:  strings.TrimSpace(values.Get("lat")),
		Lon:  strings.TrimSpace(values.Get("lon")),
	}
	if q.Lat == "" || q.Lon == "" {
		q.Lat, q.Lon = "", ""
		return q
	}
	lat, errLat := strconv.ParseFloat(q.Lat, 64)
	lon, errLon := strconv.ParseFloat(q.Lon, 64)
	if errLat == nil && errLon == nil && validLatLon(lat, lon) {
		q.HasLocation = true
		q.LatValue = lat
		q.LonValue = lon
	}
	return q
}

func bearerToken(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(header) > 7 && strings.EqualFold(header[:7], "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return strings.TrimSpace(r.Header.Get("X-Admin-Token"))
}

func (s *Server) clientIP(r *http.Request) string {
	if s.trustProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
			if net.ParseIP(first) != nil {
				return first
			}
		}
		if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); net.ParseIP(realIP) != nil {
			return realIP
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func redirectError(w http.ResponseWriter, r *http.Request, message string) {
	values := url.Values{"error": {message}}
	http.Redirect(w, r, "/?"+values.Encode(), http.StatusSeeOther)
}

func setContentType(w http.ResponseWriter, requestPath string) {
	switch strings.ToLower(urlpath.Ext(requestPath)) {
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case ".js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
	case ".woff2":
		w.Header().Set("Content-Type", "font/woff2")
	case ".woff":
		w.Header().Set("Content-Type", "font/woff")
	case ".ico":
		w.Header().Set("Content-Type", "image/x-icon")
	case ".png":
		w.Header().Set("Content-Type", "image/png")
	case ".json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
}

type broker struct {
	mu      sync.Mutex
	clients map[chan string]struct{}
}

func newBroker() *broker {
	return &broker{clients: make(map[chan string]struct{})}
}

func (b *broker) Subscribe() (<-chan string, func()) {
	ch := make(chan string, 8)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()

	return ch, func() {
		b.mu.Lock()
		if _, ok := b.clients[ch]; ok {
			delete(b.clients, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
}

func (b *broker) Broadcast(event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	message := fmt.Sprintf("event: %s\ndata: %s\n\n", event, data)

	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.clients {
		select {
		case ch <- message:
		default:
			// Slow client: drop this update rather than blocking publishing.
		}
	}
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64
	per     time.Duration
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newRateLimiter(rate int, per time.Duration) *rateLimiter {
	return &rateLimiter{
		buckets: make(map[string]*bucket),
		rate:    float64(rate),
		per:     per,
	}
}

func (l *rateLimiter) Allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.buckets) > 4096 {
		for existing, b := range l.buckets {
			if now.Sub(b.last) > l.per {
				delete(l.buckets, existing)
			}
		}
	}

	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.rate, last: now}
		l.buckets[key] = b
	}

	elapsed := now.Sub(b.last).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * (l.rate / l.per.Seconds())
		if b.tokens > l.rate {
			b.tokens = l.rate
		}
		b.last = now
	}

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
