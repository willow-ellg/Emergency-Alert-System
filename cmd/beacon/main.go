// Command beacon runs and edits the thin, standalone emergency snapshot.
//
// The beacon never talks to the hub. Operators can copy a snapshot to an
// isolated host, add an alert by hand, and keep serving it during an incident.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"emergency-alert-system/internal/beacon"
	"emergency-alert-system/internal/model"
	"emergency-alert-system/internal/seed"
	"emergency-alert-system/internal/snapshot"
	"emergency-alert-system/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "serve":
		runServe(os.Args[2:])
	case "render":
		runRender(os.Args[2:])
	case "add":
		runAdd(os.Args[2:])
	case "remove":
		runRemove(os.Args[2:])
	case "list":
		runList(os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Usage: beacon <command> [options]

Commands:
  serve    serve an existing snapshot directory (read-only)
  render   generate or refresh a snapshot from a hub state file
  add      add an alert to a snapshot directory
  remove   remove an alert from a snapshot directory
  list     list alerts in a snapshot directory

Examples:
  beacon render -dir ./snapshot
  beacon serve -dir ./snapshot -addr :8081
  beacon add -dir ./snapshot -title "Flood warning" -severity critical -summary "Move to higher ground."
  beacon list -dir ./snapshot
`)
}

func runServe(args []string) {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	dir := flags.String("dir", "./snapshot", "snapshot directory to serve")
	addr := flags.String("addr", ":8081", "HTTP listen address")
	_ = flags.Parse(args)

	server, err := beacon.New(*dir)
	if err != nil {
		log.Fatalf("open snapshot: %v", err)
	}

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Printf("shutting down beacon")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("beacon listening on %s (snapshot: %s)", *addr, server.Dir())
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve beacon: %v", err)
	}
}

func runRender(args []string) {
	flags := flag.NewFlagSet("render", flag.ExitOnError)
	dir := flags.String("dir", "./snapshot", "snapshot directory to write")
	dataPath := flags.String("data", "./data/state.json", "hub state file to read")
	demo := flags.Bool("demo", true, "add demonstration warnings and noindex metadata; pass -demo=false for live data")
	_ = flags.Parse(args)

	state, seeded, err := loadRenderState(*dataPath)
	if err != nil {
		log.Fatalf("load state: %v", err)
	}
	if seeded {
		*demo = true
		log.Printf("state file %s is empty; rendering demonstration data", *dataPath)
	}

	result, err := snapshot.Write(*dir, state, snapshot.Options{Demo: *demo})
	if err != nil {
		log.Fatalf("render snapshot: %v", err)
	}

	log.Printf("snapshot written to %s: %d active alerts (version %d, generated %s)",
		result.Directory, result.ActiveAlerts, result.Version, result.GeneratedAt.Format(time.RFC3339))
}

func loadRenderState(path string) (model.State, bool, error) {
	st, err := store.New(path)
	if err != nil {
		return model.State{}, false, err
	}
	if st.IsEmpty() {
		state := seed.Demo()
		state.Version = 1
		return state, true, nil
	}
	return st.Snapshot(), false, nil
}

func runAdd(args []string) {
	flags := flag.NewFlagSet("add", flag.ExitOnError)
	dir := flags.String("dir", "./snapshot", "snapshot directory to edit")
	title := flags.String("title", "", "alert title (required)")
	severityValue := flags.String("severity", "warning", "critical, warning or info")
	summary := flags.String("summary", "", "plain-language summary (required)")
	areas := flags.String("areas", "", "comma-separated areas; blank means national")
	guidance := flags.String("guidance", "", "guidance steps separated by | or newlines")
	source := flags.String("source", "Emergency Response Hub", "alert source")
	lat := flags.Float64("lat", 0, "latitude for a geographic alert")
	lon := flags.Float64("lon", 0, "longitude for a geographic alert")
	radius := flags.Float64("radius", 0, "radius in km; 0 disables geographic matching")
	official := flags.Bool("official", true, "mark the alert as official")
	active := flags.Bool("active", true, "publish the alert immediately")
	_ = flags.Parse(args)

	titleValue := strings.TrimSpace(*title)
	summaryValue := strings.TrimSpace(*summary)
	if titleValue == "" || summaryValue == "" {
		fmt.Fprintln(os.Stderr, "title and summary are required")
		flags.Usage()
		os.Exit(2)
	}
	if len([]rune(titleValue)) > 160 {
		log.Fatalf("title must be 160 characters or fewer")
	}
	if len([]rune(summaryValue)) > 1000 {
		log.Fatalf("summary must be 1000 characters or fewer")
	}

	severity, err := model.ParseSeverity(*severityValue)
	if err != nil {
		log.Fatal(err)
	}
	if *radius < 0 || *radius > 1000 {
		log.Fatalf("radius must be between 0 and 1000 km")
	}
	if *radius > 0 && *lat == 0 && *lon == 0 {
		log.Fatalf("set -lat and -lon when using -radius")
	}
	if *radius > 0 && (*lat < -90 || *lat > 90 || *lon < -180 || *lon > 180) {
		log.Fatalf("latitude or longitude is invalid")
	}

	state, err := beacon.Load(*dir)
	if err != nil {
		log.Fatal(err)
	}

	now := time.Now().UTC()
	alert := model.Alert{
		ID:        model.NewID("alert"),
		Title:     titleValue,
		Severity:  severity,
		Areas:     splitList(*areas, ","),
		Lat:       *lat,
		Lon:       *lon,
		RadiusKM:  *radius,
		Summary:   summaryValue,
		Guidance:  splitList(*guidance, "|"),
		Source:    strings.TrimSpace(*source),
		Official:  *official,
		Active:    *active,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if alert.Source == "" {
		alert.Source = "Emergency Response Hub"
	}

	state.Alerts = append(state.Alerts, alert)
	state.Version++

	result, err := beacon.Save(*dir, state)
	if err != nil {
		log.Fatalf("save snapshot: %v", err)
	}

	fmt.Printf("Added alert %s (%s). Snapshot now has %d active alerts.\n", alert.ID, alert.Severity, result.ActiveAlerts)
}

func runRemove(args []string) {
	flags := flag.NewFlagSet("remove", flag.ExitOnError)
	dir := flags.String("dir", "./snapshot", "snapshot directory to edit")
	id := flags.String("id", "", "alert ID to remove (required)")
	_ = flags.Parse(args)

	idValue := strings.TrimSpace(*id)
	if idValue == "" {
		fmt.Fprintln(os.Stderr, "-id is required")
		flags.Usage()
		os.Exit(2)
	}

	state, err := beacon.Load(*dir)
	if err != nil {
		log.Fatal(err)
	}

	remaining := make([]model.Alert, 0, len(state.Alerts))
	found := false
	for _, alert := range state.Alerts {
		if alert.ID == idValue {
			found = true
			continue
		}
		remaining = append(remaining, alert)
	}
	if !found {
		log.Fatalf("alert %s was not found in %s", idValue, *dir)
	}

	state.Alerts = remaining
	state.Version++

	result, err := beacon.Save(*dir, state)
	if err != nil {
		log.Fatalf("save snapshot: %v", err)
	}

	fmt.Printf("Removed alert %s. Snapshot now has %d active alerts.\n", idValue, result.ActiveAlerts)
}

func runList(args []string) {
	flags := flag.NewFlagSet("list", flag.ExitOnError)
	dir := flags.String("dir", "./snapshot", "snapshot directory to inspect")
	asJSON := flags.Bool("json", false, "print the alert data as JSON")
	_ = flags.Parse(args)

	state, err := beacon.Load(*dir)
	if err != nil {
		log.Fatal(err)
	}

	if *asJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(state.Alerts); err != nil {
			log.Fatalf("encode alerts: %v", err)
		}
		return
	}

	if len(state.Alerts) == 0 {
		fmt.Println("No alerts in this snapshot.")
		return
	}

	fmt.Printf("%-24s  %-9s  %-8s  %s\n", "ID", "SEVERITY", "STATUS", "TITLE")
	for _, alert := range state.Alerts {
		status := "active"
		if !alert.Active {
			status = "inactive"
		}
		fmt.Printf("%-24s  %-9s  %-8s  %s\n", alert.ID, alert.Severity, status, alert.Title)
	}
}

func splitList(value, separators string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == '\n' || strings.ContainsRune(separators, r)
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}
