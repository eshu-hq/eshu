// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestServiceReportsOwnedTargetRotationFreshness pins the #9775 signal: each
// owned-package read reports the rotation period (P x interval) against the
// freshness bound, so a 21-day rotation at the default limit is visible
// without reading a trace. The fake reader returns the QA-shaped window
// (T 32,726, P 1,024); the instance interval is the config's one hour.
func TestServiceReportsOwnedTargetRotationFreshness(t *testing.T) {
	t.Parallel()

	const interval = time.Hour
	tests := []struct {
		name          string
		section       string
		window        workflow.OwnedPackageDependencyTargetWindow
		wantKnown     bool
		wantPeriod    time.Duration
		wantBound     time.Duration
		wantExceeds   bool
		wantWarnReaon bool
	}{
		{
			name:      "default limit at QA scale breaches the default bound",
			section:   `{"enabled":true,"sources":["osv"],"ecosystems":["npm"]}`,
			window:    workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 32726, SlotCount: 1024, SlotSize: 32},
			wantKnown: true, wantPeriod: 1024 * interval, wantBound: schedule.DefaultOwnedTargetMaxStaleness,
			wantExceeds: true, wantWarnReaon: true,
		},
		{
			name:      "explicit bound wide enough stays quiet",
			section:   `{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"max_staleness":"2000h"}`,
			window:    workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 32726, SlotCount: 1024, SlotSize: 32},
			wantKnown: true, wantPeriod: 1024 * interval, wantBound: 2000 * time.Hour,
		},
		{
			name:      "rotation inside the default bound stays quiet",
			section:   `{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"target_limit":1500}`,
			window:    workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 32726, SlotCount: 16, SlotSize: 2000},
			wantKnown: true, wantPeriod: 16 * interval, wantBound: schedule.DefaultOwnedTargetMaxStaleness,
		},
		{
			// single_pass plans slot 0 forever and never rotates, so no
			// period exists to report.
			name:    "single pass has no rotation",
			section: `{"enabled":true,"sources":["osv"],"ecosystems":["npm"],"planning_mode":"single_pass"}`,
			window:  workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 32726, SlotCount: 1, SlotSize: 32726},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, time.November, 1, 15, 0, 0, 0, time.UTC)
			instance := testServiceVulnerabilityIntelligenceInstance(now)
			instance.Configuration = `{"derive_from_owned_packages":` + tt.section + `}`
			reader := &fakeOwnedPackageTargetReader{targets: makeOwnedNPMTargets(3), window: tt.window}
			metrics := &fakeMetrics{}
			var logs bytes.Buffer
			service := Service{
				Config:                           activeDerivedTargetConfig(scope.CollectorVulnerabilityIntelligence, instance.InstanceID, instance.Configuration),
				Store:                            &fakeStore{instances: []workflow.CollectorInstance{instance}},
				VulnerabilityIntelligencePlanner: vulnerability.IntelligenceWorkPlanner{},
				OwnedPackageTargetReader:         reader,
				Metrics:                          metrics,
				Logger:                           slog.New(slog.NewJSONHandler(&logs, nil)),
				Clock:                            func() time.Time { return now },
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := service.Run(ctx); err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}

			if got, want := len(metrics.ownedTargetRotationObserved), 1; got != want {
				t.Fatalf("rotation observations = %d, want %d", got, want)
			}
			got := metrics.ownedTargetRotationObserved[0]
			if got.CollectorInstanceID != instance.InstanceID {
				t.Errorf("CollectorInstanceID = %q, want %q", got.CollectorInstanceID, instance.InstanceID)
			}
			if got.Freshness.Known() != tt.wantKnown {
				t.Fatalf("Known() = %v, want %v", got.Freshness.Known(), tt.wantKnown)
			}
			if tt.wantKnown {
				if got.Freshness.Period != tt.wantPeriod || got.Freshness.MaxStaleness != tt.wantBound || got.Freshness.Exceeds() != tt.wantExceeds {
					t.Errorf("freshness = period %v bound %v exceeds %v, want %v %v %v",
						got.Freshness.Period, got.Freshness.MaxStaleness, got.Freshness.Exceeds(),
						tt.wantPeriod, tt.wantBound, tt.wantExceeds)
				}
			}

			warned := strings.Contains(logs.String(), `"reason":"owned_target_rotation_exceeds_max_staleness"`)
			if warned != tt.wantWarnReaon {
				t.Errorf("breach warning present = %v, want %v; logs: %s", warned, tt.wantWarnReaon, logs.String())
			}
			if tt.wantKnown {
				assertWindowLogRotationFields(t, logs.String(), tt.wantPeriod, tt.wantBound)
			}
		})
	}
}

// assertWindowLogRotationFields finds the per-read window line and checks it
// carries the rotation period and bound in seconds.
func assertWindowLogRotationFields(t *testing.T, logs string, wantPeriod, wantBound time.Duration) {
	t.Helper()

	for _, raw := range strings.Split(strings.TrimSpace(logs), "\n") {
		var line struct {
			Msg    string  `json:"msg"`
			Period float64 `json:"rotation_period_seconds"`
			Bound  float64 `json:"max_staleness_seconds"`
		}
		if err := json.Unmarshal([]byte(raw), &line); err != nil || line.Msg != "workflow coordinator read owned package target window" {
			continue
		}
		if line.Period != wantPeriod.Seconds() || line.Bound != wantBound.Seconds() {
			t.Errorf("window line rotation_period_seconds=%v max_staleness_seconds=%v, want %v and %v",
				line.Period, line.Bound, wantPeriod.Seconds(), wantBound.Seconds())
		}
		return
	}
	t.Errorf("no owned package window line in logs: %s", logs)
}

// TestActivationReportsOwnedTargetRotationFreshness covers the activation call
// site of the #9775 signal. A vulnerability activation reads its owned-package
// page through its own path, separate from the in-tree instance loop, so the
// report must be wired there too: one observation with the rotation period
// (P x the activation interval) and the default bound, flagged as a breach.
func TestActivationReportsOwnedTargetRotationFreshness(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 1, 15, 0, 0, 0, time.UTC)
	instance := vulnerabilityActivationWithDerive(now,
		`{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"]}}`)
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	reader := ownedViteReader()
	reader.window = workflow.OwnedPackageDependencyTargetWindow{TotalTargets: 32726, SlotCount: 1024, SlotSize: 32}
	service := testVulnerabilityActivationService(t, now, instance, store, reader, `{"mode":"broad"}`)
	metrics := &fakeMetrics{}
	service.Metrics = metrics

	if err := runCancelledReconcile(t, service); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}

	if got, want := len(metrics.ownedTargetRotationObserved), 1; got != want {
		t.Fatalf("rotation observations = %d, want %d", got, want)
	}
	got := metrics.ownedTargetRotationObserved[0]
	if got.CollectorInstanceID != instance.InstanceID {
		t.Errorf("CollectorInstanceID = %q, want %q", got.CollectorInstanceID, instance.InstanceID)
	}
	if !got.Freshness.Known() {
		t.Fatalf("Known() = false, want true for a 1,024-slot window")
	}
	wantPeriod := 1024 * time.Hour // activation interval is Config.ReconcileInterval (1h)
	if got.Freshness.Period != wantPeriod || got.Freshness.MaxStaleness != schedule.DefaultOwnedTargetMaxStaleness || !got.Freshness.Exceeds() {
		t.Errorf("freshness = period %v bound %v exceeds %v, want %v %v true",
			got.Freshness.Period, got.Freshness.MaxStaleness, got.Freshness.Exceeds(),
			wantPeriod, schedule.DefaultOwnedTargetMaxStaleness)
	}
}
