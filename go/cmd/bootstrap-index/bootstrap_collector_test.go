// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestDrainCollectorWithTelemetry(t *testing.T) {
	t.Parallel()

	source := &fakeSource{
		generations: []collector.CollectedGeneration{
			{
				Scope:              scope.IngestionScope{ScopeID: "s1"},
				Facts:              testFactChannel(facts.Envelope{}, facts.Envelope{}),
				EstimatedFactCount: 2,
			},
		},
	}

	err := drainCollector(
		context.Background(),
		source, &fakeCommitter{},
		nil, nil, nil, 1,
	)
	if err != nil {
		t.Fatalf("drainCollector() error = %v, want nil", err)
	}
}

func TestDrainCollectorCollectsDiscoveryAdvisoryReports(t *testing.T) {
	t.Parallel()

	source := &fakeSource{generations: []collector.CollectedGeneration{{
		Scope: scope.IngestionScope{ScopeID: "scope-1"},
		Generation: scope.ScopeGeneration{
			GenerationID: "generation-1",
		},
		EstimatedFactCount: 0,
		DiscoveryAdvisory: &collector.DiscoveryAdvisoryReport{
			SchemaVersion: "discovery_advisory.v1",
			Run: collector.DiscoveryAdvisoryRun{
				RepoPath: "/repo",
			},
		},
	}}}
	var reports []collector.DiscoveryAdvisoryReport

	err := drainCollector(context.Background(), source, &fakeCommitter{}, nil, nil, nil, 1, func(report collector.DiscoveryAdvisoryReport) error {
		reports = append(reports, report)
		return nil
	})
	if err != nil {
		t.Fatalf("drainCollector() error = %v, want nil", err)
	}

	if got, want := len(reports), 1; got != want {
		t.Fatalf("report count = %d, want %d", got, want)
	}
	if got, want := reports[0].Run.ScopeID, "scope-1"; got != want {
		t.Fatalf("report Run.ScopeID = %q, want %q", got, want)
	}
	if got, want := reports[0].Run.GenerationID, "generation-1"; got != want {
		t.Fatalf("report Run.GenerationID = %q, want %q", got, want)
	}
}

func TestWriteDiscoveryAdvisoryReportsWritesJSON(t *testing.T) {
	t.Parallel()

	reportPath := filepath.Join(t.TempDir(), "advisory.json")
	generatedAt := time.Date(2026, time.April, 26, 10, 30, 0, 0, time.UTC)
	reports := []collector.DiscoveryAdvisoryReport{{
		SchemaVersion: "discovery_advisory.v1",
		GeneratedAt:   generatedAt,
		Run: collector.DiscoveryAdvisoryRun{
			RepoPath: "/repo",
			ScopeID:  "scope-1",
		},
	}}

	if err := writeDiscoveryAdvisoryReports(reportPath, reports); err != nil {
		t.Fatalf("writeDiscoveryAdvisoryReports() error = %v, want nil", err)
	}

	contents, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if !strings.Contains(string(contents), `"schema_version": "discovery_advisory.v1"`) {
		t.Fatalf("report contents missing schema version:\n%s", contents)
	}
	if !strings.Contains(string(contents), `"scope_id": "scope-1"`) {
		t.Fatalf("report contents missing scope id:\n%s", contents)
	}
}
