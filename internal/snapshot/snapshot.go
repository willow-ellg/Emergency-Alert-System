// Package snapshot renders the thin, standalone emergency system.
//
// A snapshot is a self-contained directory of static files. It has no database,
// no server-side dependencies and no network requirements: any static file
// server (or even a USB stick) can deliver it during an incident.
package snapshot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"

	"emergency-alert-system/internal/model"
	"emergency-alert-system/internal/view"
	"emergency-alert-system/web"
)

// Options controls snapshot rendering.
type Options struct {
	// Demo adds the demonstration warning and the noindex robots meta tag.
	Demo bool
	// Now pins the generation time and all relative timestamps. It defaults to
	// the current time.
	Now time.Time
}

// Result describes a generated snapshot.
type Result struct {
	Directory    string    `json:"directory"`
	ActiveAlerts int       `json:"active_alerts"`
	GeneratedAt  time.Time `json:"generated_at"`
	Version      int       `json:"version"`
}

type pageData struct {
	Title       string
	AssetPrefix string
	HomeURL     string
	Static      bool
	ExtraScript string
	PhaseTag    string
	PhaseText   string
	Now         time.Time
	Demo        bool

	GeneratedAt time.Time
	Version     int
	Alerts      []model.Alert
	Myths       []model.Myth
}

type alertsFile struct {
	GeneratedAt time.Time     `json:"generated_at"`
	Version     int           `json:"version"`
	Demo        bool          `json:"demo"`
	Alerts      []model.Alert `json:"alerts"`
}

type mythsFile struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Version     int          `json:"version"`
	Myths       []model.Myth `json:"myths"`
}

type metadata struct {
	GeneratedAt  time.Time `json:"generated_at"`
	Version      int       `json:"version"`
	ActiveAlerts int       `json:"active_alerts"`
	Demo         bool      `json:"demo"`
}

// Write renders a complete static snapshot into dir.
//
// Files are replaced one at a time, and index.html is written last. That means
// a running thin server can keep serving the previous snapshot while a new one
// is being generated.
func Write(dir string, state model.State, opts Options) (Result, error) {
	if dir == "" {
		return Result{}, fmt.Errorf("snapshot directory is required")
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, fmt.Errorf("create snapshot directory: %w", err)
	}

	alerts := make([]model.Alert, 0, len(state.Alerts))
	for _, alert := range state.Alerts {
		if alert.Active {
			alerts = append(alerts, alert.Clone())
		}
	}
	model.SortAlerts(alerts)

	if err := writeJSONAtomic(filepath.Join(dir, "data", "alerts.json"), alertsFile{
		GeneratedAt: now,
		Version:     state.Version,
		Demo:        opts.Demo,
		Alerts:      alerts,
	}); err != nil {
		return Result{}, fmt.Errorf("write alerts data: %w", err)
	}

	if err := writeJSONAtomic(filepath.Join(dir, "data", "myths.json"), mythsFile{
		GeneratedAt: now,
		Version:     state.Version,
		Myths:       state.Myths,
	}); err != nil {
		return Result{}, fmt.Errorf("write myths data: %w", err)
	}

	if err := writeStatic(dir); err != nil {
		return Result{}, err
	}

	funcs := view.FuncMap(func() time.Time { return now })
	tmpl, err := template.New("layout").Funcs(funcs).ParseFS(web.FS,
		"templates/layout.html",
		"templates/snapshot.html",
	)
	if err != nil {
		return Result{}, fmt.Errorf("parse snapshot templates: %w", err)
	}

	phaseText := "Read-only emergency information. This page does not update automatically."
	if opts.Demo {
		phaseText = "Read-only demonstration snapshot. This is not live emergency information."
	}

	data := pageData{
		Title:       "Emergency information snapshot",
		AssetPrefix: "",
		HomeURL:     "./",
		Static:      true,
		PhaseTag:    "Emergency snapshot",
		PhaseText:   phaseText,
		Now:         now,
		Demo:        opts.Demo,
		GeneratedAt: now,
		Version:     state.Version,
		Alerts:      alerts,
		Myths:       state.Myths,
	}

	var page bytes.Buffer
	if err := tmpl.ExecuteTemplate(&page, "layout", data); err != nil {
		return Result{}, fmt.Errorf("render snapshot page: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(dir, "index.html"), page.Bytes(), 0o644); err != nil {
		return Result{}, fmt.Errorf("write snapshot page: %w", err)
	}

	if err := writeJSONAtomic(filepath.Join(dir, "snapshot.json"), metadata{
		GeneratedAt:  now,
		Version:      state.Version,
		ActiveAlerts: len(alerts),
		Demo:         opts.Demo,
	}); err != nil {
		return Result{}, fmt.Errorf("write snapshot metadata: %w", err)
	}

	return Result{
		Directory:    dir,
		ActiveAlerts: len(alerts),
		GeneratedAt:  now,
		Version:      state.Version,
	}, nil
}

func writeStatic(dir string) error {
	css, err := fs.ReadFile(web.FS, "static/govuk/govuk-frontend.min.css")
	if err != nil {
		return fmt.Errorf("read GOV.UK CSS: %w", err)
	}
	css = bytes.ReplaceAll(css, []byte("url(/assets/fonts/"), []byte("url(../fonts/"))
	css = bytes.ReplaceAll(css, []byte("url(/assets/images/"), []byte("url(../images/"))
	if err := writeFileAtomic(filepath.Join(dir, "govuk", "govuk-frontend.min.css"), css, 0o644); err != nil {
		return fmt.Errorf("write GOV.UK CSS: %w", err)
	}

	siteCSS, err := fs.ReadFile(web.FS, "static/site.css")
	if err != nil {
		return fmt.Errorf("read site CSS: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(dir, "site.css"), siteCSS, 0o644); err != nil {
		return fmt.Errorf("write site CSS: %w", err)
	}

	manifest, err := fs.ReadFile(web.FS, "static/manifest.json")
	if err != nil {
		return fmt.Errorf("read web manifest: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(dir, "manifest.json"), manifest, 0o644); err != nil {
		return fmt.Errorf("write web manifest: %w", err)
	}

	for _, name := range []string{"fonts", "images"} {
		if err := copyDir(dir, path.Join("static", name), name); err != nil {
			return err
		}
	}

	return nil
}

func copyDir(destRoot, embedPath, relative string) error {
	entries, err := fs.ReadDir(web.FS, embedPath)
	if err != nil {
		return fmt.Errorf("read embedded %s: %w", relative, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := fs.ReadFile(web.FS, path.Join(embedPath, entry.Name()))
		if err != nil {
			return fmt.Errorf("read embedded %s/%s: %w", relative, entry.Name(), err)
		}
		if err := writeFileAtomic(filepath.Join(destRoot, relative, entry.Name()), data, 0o644); err != nil {
			return fmt.Errorf("write %s/%s: %w", relative, entry.Name(), err)
		}
	}
	return nil
}

func writeJSONAtomic(name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(name, data, 0o644)
}

func writeFileAtomic(name string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".snapshot-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, name)
}
