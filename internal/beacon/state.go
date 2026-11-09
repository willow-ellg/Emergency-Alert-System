package beacon

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"emergency-alert-system/internal/model"
	"emergency-alert-system/internal/snapshot"
)

// State is the editable portion of a snapshot. It deliberately excludes
// pending or rejected public reports so the thin system can be copied and
// served without carrying moderation data.
type State struct {
	Alerts  []model.Alert
	Myths   []model.Myth
	Version int
	Demo    bool
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

// Load reads the editable state from an existing snapshot directory.
func Load(dir string) (State, error) {
	alerts, err := readAlerts(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, fmt.Errorf("no snapshot found in %s; run 'beacon render -dir %s' first", dir, dir)
		}
		return State{}, err
	}

	state := State{
		Alerts:  alerts.Alerts,
		Version: alerts.Version,
		Demo:    alerts.Demo,
	}

	myths, err := readMyths(dir)
	if err == nil {
		state.Myths = myths.Myths
	} else if !errors.Is(err, os.ErrNotExist) {
		return State{}, err
	}

	return state, nil
}

// Save rewrites the snapshot with the supplied state.
func Save(dir string, state State) (snapshot.Result, error) {
	return snapshot.Write(dir, model.State{
		Alerts:  state.Alerts,
		Myths:   state.Myths,
		Version: state.Version,
	}, snapshot.Options{
		Demo: state.Demo,
		Now:  time.Now(),
	})
}

func readAlerts(dir string) (alertsFile, error) {
	var out alertsFile
	data, err := os.ReadFile(filepath.Join(dir, "data", "alerts.json"))
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("parse data/alerts.json: %w", err)
	}
	return out, nil
}

func readMyths(dir string) (mythsFile, error) {
	var out mythsFile
	data, err := os.ReadFile(filepath.Join(dir, "data", "myths.json"))
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("parse data/myths.json: %w", err)
	}
	return out, nil
}

func readMetadata(dir string) (metadata, error) {
	var out metadata
	data, err := os.ReadFile(filepath.Join(dir, "snapshot.json"))
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, fmt.Errorf("parse snapshot.json: %w", err)
	}
	return out, nil
}
