// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parity

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestReadReportPlaneLiveCompat proves the five read/report routes against
// the bound satellite build: it seeds one run with two items through the
// satellite leg, pushes one completeness checkpoint and one progress report,
// then reads every route back through the in-tree client and requires the
// seeded values to agree. It runs only with the satellite leg configured
// (local flip container or dark deployment); without it the test skips.
//
// The suite run id carries the parity- prefix, so the dark-leg cleanup
// deletes only this test's rows and the local leg truncates.
func TestReadReportPlaneLiveCompat(t *testing.T) {
	satelliteURL := os.Getenv("PARITY_SATELLITE_URL")
	satelliteDSN := os.Getenv("PARITY_SATELLITE_DSN")
	if satelliteURL == "" || satelliteDSN == "" {
		t.Skip("PARITY_SATELLITE_URL/DSN is not set; skipping live read/report compat")
	}
	ctx := context.Background()
	ResetSatellite(t, satelliteDSN)
	t.Cleanup(func() { ResetSatellite(t, satelliteDSN) })
	satellite := OpenSatellite(t, satelliteURL)

	suite := NewSuite(t, "read-report")
	suite.SeedRun(ctx, satellite)
	suite.SeedItems(ctx, satellite, 2)
	if err := satellite.UpsertCompletenessStates(ctx, []workflow.CompletenessState{{
		RunID: suite.RunID, CollectorKind: scope.CollectorKind("jira"),
		Keyspace: "graph", PhaseName: "canonical", Required: true,
		Status:     workflow.CompletenessStatusPending,
		Detail:     "compat probe",
		ObservedAt: suite.Now, UpdatedAt: suite.Now,
	}}); err != nil {
		t.Fatalf("upsert completeness: %v", err)
	}

	client := workflow.NewClaimPlaneClient(satelliteURL, &http.Client{Timeout: satelliteLegTimeout})
	progress := []workflow.CollectorRunProgress{{
		CollectorKind:  scope.CollectorKind("jira"),
		TotalWorkItems: 2, PendingWorkItems: 2,
		PublishedPhaseCounts: map[workflow.PhasePublicationKey]int{
			{Keyspace: "graph", PhaseName: "canonical"}: 0,
		},
	}}
	if err := client.ReportRunProgress(ctx, suite.RunID, suite.Now, progress); err != nil {
		t.Fatalf("report run progress: %v", err)
	}

	status, err := client.GetRunStatus(ctx, suite.RunID)
	if err != nil {
		t.Fatalf("get run status: %v", err)
	}
	if status.Run.RunID != suite.RunID {
		t.Fatalf("status run = %q, want %q", status.Run.RunID, suite.RunID)
	}
	if len(status.Progress) != 1 || status.Progress[0].TotalWorkItems != 2 {
		t.Fatalf("status progress = %+v, want one family of 2", status.Progress)
	}
	if len(status.Completeness) != 1 || status.Completeness[0].PhaseName != "canonical" {
		t.Fatalf("status completeness = %+v, want one canonical checkpoint", status.Completeness)
	}

	completeness, err := client.GetRunCompleteness(ctx, suite.RunID)
	if err != nil {
		t.Fatalf("get run completeness: %v", err)
	}
	if completeness.RunID != suite.RunID || len(completeness.States) != 1 {
		t.Fatalf("completeness = %+v, want %q with one state", completeness, suite.RunID)
	}

	page, err := client.ListRuns(ctx, "", 10, "")
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	found := false
	for _, run := range page.Runs {
		if run.RunID == suite.RunID {
			found = true
		}
	}
	if !found {
		t.Fatalf("list runs missing %q in %+v", suite.RunID, page.Runs)
	}

	depth, err := client.QueueDepthSnapshot(ctx)
	if err != nil {
		t.Fatalf("queue depth snapshot: %v", err)
	}
	if depth.GlobalPending < 2 {
		t.Fatalf("global pending = %d, want at least the 2 seeded items", depth.GlobalPending)
	}

	if _, err := client.GetRunStatus(ctx, "parity-read-report-missing"); err == nil {
		t.Fatal("get run status on a missing run succeeded, want not-found")
	}
}
