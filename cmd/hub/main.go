// Command hub runs the full emergency response hub.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"emergency-alert-system/internal/hub"
	"emergency-alert-system/internal/seed"
	"emergency-alert-system/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	dataPath := flag.String("data", "./data/state.json", "path to the JSON state file")
	snapshotDir := flag.String("snapshot-dir", "./snapshot", "directory for generated emergency snapshots")
	seedOnly := flag.Bool("seed-only", false, "seed demonstration data and exit")
	demo := flag.Bool("demo", true, "show demonstration warnings and noindex metadata")
	trustProxy := flag.Bool("trust-proxy", false, "trust X-Forwarded-For/X-Real-IP from a reverse proxy for rate limiting")
	flag.Parse()

	st, err := store.New(*dataPath)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}

	if st.IsEmpty() {
		if err := st.Seed(seed.Demo()); err != nil {
			log.Fatalf("seed demonstration data: %v", err)
		}
		log.Printf("seeded demonstration data at %s", *dataPath)
		if !*demo {
			log.Printf("warning: refusing to hide the demonstration warning for seeded data")
			*demo = true
		}
	}

	if *seedOnly {
		return
	}

	adminToken := os.Getenv("HUB_ADMIN_TOKEN")
	if adminToken == "" {
		log.Printf("warning: HUB_ADMIN_TOKEN is empty; the operator API is disabled")
	}

	server, err := hub.New(st, hub.Options{
		AdminToken:  adminToken,
		SnapshotDir: *snapshotDir,
		Demo:        *demo,
		TrustProxy:  *trustProxy,
	})
	if err != nil {
		log.Fatalf("create hub: %v", err)
	}

	httpServer := &http.Server{
		Addr:              *addr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// WriteTimeout stays zero because /api/stream is a long-lived SSE
		// connection. Individual handlers use their own deadlines where needed.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Printf("shutting down hub")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("hub listening on %s (data: %s, snapshots: %s)", *addr, *dataPath, *snapshotDir)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("serve hub: %v", err)
	}
}
