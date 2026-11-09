// Package seed provides clearly-labelled demonstration data.
//
// Every alert, report and myth below is fictional. The public UI displays a
// permanent prototype warning so this data can never be mistaken for live
// emergency information.
package seed

import (
	"time"

	"emergency-alert-system/internal/model"
)

// Demo returns a complete demonstration dataset.
func Demo() model.State {
	now := time.Now().UTC()

	alerts := []model.Alert{
		{
			ID:       "alert-demo-flood",
			Title:    "Flood warning: Riverside and Lowbridge",
			Severity: model.SeverityCritical,
			Areas:    []string{"Riverside", "Lowbridge", "Riverside East"},
			Lat:      51.501,
			Lon:      -0.100,
			RadiusKM: 12,
			Summary: "River levels are rising after heavy rain. Properties near the river may flood. " +
				"DEMONSTRATION DATA - this is not a real incident.",
			Guidance: []string{
				"Move family members, pets and essential medication to higher ground.",
				"Do not drive or walk through flood water. It can hide debris and fast currents.",
				"Check on neighbours who may need extra help.",
				"Call 999 only if there is an immediate risk to life.",
				"Follow updates from the demonstration Environment Agency.",
			},
			Source:    "Demo Environment Agency",
			Official:  true,
			Active:    true,
			CreatedAt: now.Add(-3 * time.Hour),
			UpdatedAt: now.Add(-20 * time.Minute),
		},
		{
			ID:       "alert-demo-power",
			Title:    "Power outage: Northgate",
			Severity: model.SeverityWarning,
			Areas:    []string{"Northgate", "Northgate Industrial Park"},
			Summary: "Power is off in parts of Northgate. Engineers are working to restore supply. " +
				"DEMONSTRATION DATA - this is not a real incident.",
			Guidance: []string{
				"Keep fridge and freezer doors closed to preserve food.",
				"Use torches rather than candles to reduce fire risk.",
				"Check on vulnerable neighbours.",
				"Treat any damaged power line as live and stay well clear.",
			},
			Source:    "Demo Power Network",
			Official:  true,
			Active:    true,
			CreatedAt: now.Add(-90 * time.Minute),
			UpdatedAt: now.Add(-10 * time.Minute),
		},
		{
			ID:       "alert-demo-test",
			Title:    "National alert system test",
			Severity: model.SeverityInfo,
			Summary: "A demonstration test of the national alerting channel. No action is required. " +
				"DEMONSTRATION DATA - this is not a real incident.",
			Guidance: []string{
				"You do not need to do anything.",
				"If you did not receive a test message, no action is required.",
				"This is a demonstration of the public awareness hub.",
			},
			Source:    "Demo Civil Contingencies Unit",
			Official:  true,
			Active:    true,
			CreatedAt: now.Add(-24 * time.Hour),
			UpdatedAt: now.Add(-6 * time.Hour),
		},
		{
			ID:        "alert-demo-closed",
			Title:     "Road closure: Harbour Road (resolved)",
			Severity:  model.SeverityInfo,
			Areas:     []string{"Harbour"},
			Summary:   "Harbour Road has reopened. This archived alert is kept for the demonstration.",
			Guidance:  []string{"No action is required."},
			Source:    "Demo Highways Team",
			Official:  true,
			Active:    false,
			CreatedAt: now.Add(-48 * time.Hour),
			UpdatedAt: now.Add(-30 * time.Hour),
		},
	}

	reports := []model.Report{
		{
			ID:        "report-demo-bridge",
			AlertID:   "alert-demo-flood",
			Area:      "Riverside",
			Category:  "road",
			Body:      "Riverside Bridge is closed to traffic. Use the Lowbridge bypass instead.",
			Status:    model.ReportApproved,
			Verified:  true,
			CreatedAt: now.Add(-55 * time.Minute),
		},
		{
			ID:        "report-demo-rest-centre",
			AlertID:   "alert-demo-flood",
			Area:      "Lowbridge",
			Category:  "shelter",
			Body:      "A rest centre is open at Lowbridge Community Hall. Hot drinks and charging points are available.",
			Status:    model.ReportApproved,
			Verified:  true,
			CreatedAt: now.Add(-40 * time.Minute),
		},
		{
			ID:        "report-demo-water",
			AlertID:   "alert-demo-flood",
			Area:      "Lowbridge",
			Category:  "hazard",
			Body:      "Water is rising quickly near the old mill. The footpath is under water.",
			Status:    model.ReportPending,
			CreatedAt: now.Add(-15 * time.Minute),
		},
	}

	myths := []model.Myth{
		{
			ID:         "myth-demo-evacuation",
			Claim:      "The whole city is being evacuated.",
			Correction: "No city-wide evacuation has been ordered. Only properties in Riverside and Lowbridge are affected. DEMONSTRATION DATA.",
			Source:     "Demo Civil Contingencies Unit",
			UpdatedAt:  now.Add(-25 * time.Minute),
		},
		{
			ID:         "myth-demo-water",
			Claim:      "Tap water is unsafe to drink across the whole area.",
			Correction: "No boil-water notice has been issued. Follow your water supplier's official advice. DEMONSTRATION DATA.",
			Source:     "Demo Water Authority",
			UpdatedAt:  now.Add(-35 * time.Minute),
		},
		{
			ID:         "myth-demo-response",
			Claim:      "Emergency services are not responding.",
			Correction: "Emergency services are responding. Call 999 if there is an immediate risk to life. DEMONSTRATION DATA.",
			Source:     "Demo Police Service",
			UpdatedAt:  now.Add(-45 * time.Minute),
		},
	}

	return model.State{
		Alerts:    alerts,
		Reports:   reports,
		Myths:     myths,
		UpdatedAt: now,
	}
}
