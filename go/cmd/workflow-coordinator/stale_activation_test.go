// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/coordinator"
)

type staleSkipFakeMetrics struct {
	observations []coordinator.StaleActivationSkipObservation
}

func (f *staleSkipFakeMetrics) RecordReconcile(_ context.Context, _ coordinator.ReconcileObservation) {
}

func (f *staleSkipFakeMetrics) RecordReap(_ context.Context, _ coordinator.ReapObservation) {
}

func (f *staleSkipFakeMetrics) RecordStaleActivationSkip(_ context.Context, observation coordinator.StaleActivationSkipObservation) {
	f.observations = append(f.observations, observation)
}

func (f *staleSkipFakeMetrics) RecordRunReconciliation(_ context.Context, _ coordinator.RunReconciliationObservation) {
}

func (f *staleSkipFakeMetrics) RecordAWSFreshnessReap(_ context.Context, _ coordinator.FreshnessReapObservation) {
}

func (f *staleSkipFakeMetrics) RecordGCPFreshnessReap(_ context.Context, _ coordinator.FreshnessReapObservation) {
}

func (f *staleSkipFakeMetrics) RecordIncidentFreshnessReap(_ context.Context, _ coordinator.FreshnessReapObservation) {
}

func (f *staleSkipFakeMetrics) RecordScannerDerivedPage(_ context.Context, _ coordinator.ScannerDerivedPageObservation) {
}

func writeStaleRegistryHome(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	state := `{
  "components": [
    {
      "id": "dev.eshu.examples.scorecard",
      "name": "Reference Scorecard collector",
      "publisher": "eshu-hq",
      "version": "0.1.0",
      "manifest_path": "/nonexistent/manifest.yaml",
      "manifest_digest": "abc",
      "verified": true,
      "trust_mode": "allowlist",
      "installed_at": "2026-10-30T00:00:00Z",
      "activations": [
        {
          "instance_id": "scorecard-stale",
          "mode": "scheduled",
          "claims_enabled": true,
          "config_path": "/nonexistent/scorecard.yaml",
          "enabled_at": "2026-10-30T00:00:00Z"
        }
      ]
    }
  ]
}`
	if err := os.WriteFile(filepath.Join(home, "registry.json"), []byte(state), 0o600); err != nil {
		t.Fatalf("write registry state: %v", err)
	}
	return home
}

func TestReportStaleActivationSkipsReconcilesWarnsAndCounts(t *testing.T) {
	t.Parallel()

	home := writeStaleRegistryHome(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	metrics := &staleSkipFakeMetrics{}
	skips := []coordinator.StaleActivationSkip{{
		ComponentID: "dev.eshu.examples.scorecard",
		InstanceID:  "scorecard-stale",
	}}

	reportStaleActivationSkips(context.Background(), metrics, logger, home, skips)

	if got, want := len(metrics.observations), 1; got != want {
		t.Fatalf("stale skip observations = %d, want %d", got, want)
	}
	observation := metrics.observations[0]
	if !observation.Reconciled {
		t.Fatalf("stale skip observation = %+v, want reconciled", observation)
	}
	if got := logs.String(); !strings.Contains(got, "scorecard-stale") {
		t.Fatalf("warning log = %q, want stale instance id", got)
	}

	raw, err := os.ReadFile(filepath.Join(home, "registry.json"))
	if err != nil {
		t.Fatalf("read registry state: %v", err)
	}
	if strings.Contains(string(raw), "scorecard-stale") {
		t.Fatalf("registry state still references stale activation: %s", raw)
	}
}

func TestReportStaleActivationSkipsEmptyIsSilent(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	metrics := &staleSkipFakeMetrics{}

	reportStaleActivationSkips(context.Background(), metrics, logger, t.TempDir(), nil)

	if got := len(metrics.observations); got != 0 {
		t.Fatalf("stale skip observations = %d, want 0", got)
	}
	if got := logs.String(); got != "" {
		t.Fatalf("warning log = %q, want empty", got)
	}
}

func TestReportStaleActivationSkipsCountsHealedEntryAsReconciled(t *testing.T) {
	t.Parallel()

	// The entry is already gone from the registry: nothing to drop, and
	// the observation still reports reconciled rather than an error.
	home := writeStaleRegistryHome(t)
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	metrics := &staleSkipFakeMetrics{}

	reportStaleActivationSkips(context.Background(), metrics, logger, home, []coordinator.StaleActivationSkip{{
		ComponentID: "dev.eshu.examples.scorecard",
		InstanceID:  "scorecard-already-gone",
	}})

	if got, want := len(metrics.observations), 1; got != want {
		t.Fatalf("stale skip observations = %d, want %d", got, want)
	}
	if !metrics.observations[0].Reconciled {
		t.Fatalf("stale skip observation = %+v, want reconciled for an already-gone entry", metrics.observations[0])
	}
	if got := logs.String(); strings.Contains(got, `"error"`) || strings.Contains(got, "error=") {
		t.Fatalf("warning log = %q, want no error for an already-gone entry", got)
	}
}

func TestReportStaleActivationSkipsCountsFailedDropAsError(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "registry.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt registry: %v", err)
	}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	metrics := &staleSkipFakeMetrics{}

	reportStaleActivationSkips(context.Background(), metrics, logger, home, []coordinator.StaleActivationSkip{{
		ComponentID: "dev.eshu.examples.scorecard",
		InstanceID:  "scorecard-stale",
	}})

	if got, want := len(metrics.observations), 1; got != want {
		t.Fatalf("stale skip observations = %d, want %d", got, want)
	}
	if metrics.observations[0].Reconciled {
		t.Fatalf("stale skip observation = %+v, want unreconciled for a failed drop", metrics.observations[0])
	}
	if got := logs.String(); !strings.Contains(got, "scorecard-stale") {
		t.Fatalf("warning log = %q, want stale instance id", got)
	}
}
