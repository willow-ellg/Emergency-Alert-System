// Package beacon implements the thin, standalone emergency server.
//
// It serves a pre-generated snapshot directory. It has no database, no
// templates, no writes and no dependency on the full hub, so it can keep
// working when the main platform is unavailable.
package beacon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	urlpath "path"
	"path/filepath"
	"strings"
)

// Server serves a snapshot directory over HTTP.
type Server struct {
	dir   string
	files http.Handler
}

// New opens a snapshot directory for serving.
func New(dir string) (*Server, error) {
	if dir == "" {
		return nil, errors.New("snapshot directory is required")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve snapshot directory: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("open snapshot directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("snapshot path %s is not a directory", absolute)
	}
	return &Server{dir: absolute, files: http.FileServer(http.Dir(absolute))}, nil
}

// Dir returns the absolute snapshot directory.
func (s *Server) Dir() string { return s.dir }

// Handler returns the read-only HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/alerts", s.handleAlerts)
	mux.HandleFunc("GET /", s.handleFile)
	return s.securityHeaders(mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	index := filepath.Join(s.dir, "index.html")
	if _, err := os.Stat(index); err != nil {
		s.notReady(w)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, index)
}

func (s *Server) handleFile(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/") {
		http.NotFound(w, r)
		return
	}
	for _, segment := range strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/") {
		if strings.HasPrefix(segment, ".") {
			http.NotFound(w, r)
			return
		}
	}

	setContentType(w, r.URL.Path)
	if strings.HasPrefix(r.URL.Path, "/data/") || r.URL.Path == "/snapshot.json" {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=300")
	}
	s.files.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	meta, err := readMetadata(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "empty",
				"mode":   "beacon",
			})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"status": "error",
			"mode":   "beacon",
			"error":  "snapshot metadata could not be read",
		})
		return
	}

	if _, err := os.Stat(filepath.Join(s.dir, "index.html")); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "empty",
			"mode":   "beacon",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"mode":         "beacon",
		"generated_at": meta.GeneratedAt,
		"version":      meta.Version,
		"alerts":       meta.ActiveAlerts,
		"demo":         meta.Demo,
	})
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	alerts, err := readAlerts(s.dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no snapshot alerts found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "snapshot alerts could not be read"})
		return
	}
	writeJSON(w, http.StatusOK, alerts.Alerts)
}

func (s *Server) notReady(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("Emergency snapshot is not ready. Generate one with: beacon render -dir <snapshot-directory>\n"))
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'none'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'; object-src 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), payment=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
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
	case ".html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
}
