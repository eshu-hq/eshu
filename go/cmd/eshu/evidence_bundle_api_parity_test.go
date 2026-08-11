// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

// TestEvidenceBundleAPIRouteMatchesCLILiveExport is the #4045 acceptance test
// for "console and CLI both link to or generate the same artifact": it proves
// GET /api/v0/evidence/bundle and `eshu evidence bundle export --live`
// produce structurally identical bundles from one fixed status snapshot.
//
// Both paths are driven against the SAME real handlers -- query.StatusHandler
// and query.EvidenceHandler mounted on one httptest server, backed by the
// SAME status.Reader and GraphQuery fixtures -- so this is not two
// independently maintained readings compared by inspection; it is one
// snapshot read through two entry points into the same composer
// (evidencebundle.BuildLiveBundle -> Validate -> StampValidation).

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/evidencebundle"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// parityStatusReader is a deterministic status.Reader fixture: one stage, one
// domain backlog with two matching queue blockages, one queue, one
// coordinator-registered collector, and a not-configured semantic provider.
type parityStatusReader struct {
	snapshot status.RawSnapshot
}

func (r parityStatusReader) ReadStatusSnapshot(_ context.Context, _ time.Time) (status.RawSnapshot, error) {
	return r.snapshot, nil
}

func (r parityStatusReader) ReadStatusSnapshotFiltered(
	ctx context.Context,
	asOf time.Time,
	_ status.SnapshotSelection,
) (status.RawSnapshot, error) {
	return r.ReadStatusSnapshot(ctx, asOf)
}

// parityGraphQuery is a query.GraphQuery fixture returning a fixed
// repository count for the "MATCH (r:Repository) RETURN count(r)" query both
// GET /api/v0/status/index and GET /api/v0/evidence/bundle run.
type parityGraphQuery struct {
	count int
}

func (g parityGraphQuery) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	return nil, nil
}

func (g parityGraphQuery) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return map[string]any{"count": g.count}, nil
}

func evidenceBundleParityRawSnapshot() status.RawSnapshot {
	fixed := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	return status.RawSnapshot{
		AsOf: fixed,
		ScopeActivity: status.ScopeActivitySnapshot{
			Active: 5, Changed: 1, Unchanged: 4,
		},
		GenerationHistory: status.GenerationHistorySnapshot{
			Active: 1, Completed: 9, Superseded: 5, Other: 2,
		},
		StageCounts: []status.StageStatusCount{
			{Stage: "parse", Status: "pending", Count: 2},
			{Stage: "parse", Status: "claimed", Count: 6},
			{Stage: "parse", Status: "running", Count: 3},
			{Stage: "parse", Status: "succeeded", Count: 11},
		},
		DomainBacklogs: []status.DomainBacklog{
			{
				Domain:      "aws_relationship_materialization",
				Outstanding: 5,
				InFlight:    4,
				DeadLetter:  1,
				OldestAge:   41500 * time.Millisecond,
			},
		},
		QueueBlockages: []status.QueueBlockage{
			{Stage: "reduce", Domain: "aws_relationship_materialization", ConflictDomain: "aws", ConflictKey: "k1", Blocked: 3},
			{Stage: "reduce", Domain: "aws_relationship_materialization", ConflictDomain: "aws", ConflictKey: "k2", Blocked: 2},
		},
		Queue: status.QueueSnapshot{
			Total: 18, Outstanding: 7, Pending: 4, InFlight: 2, Retrying: 1,
			Succeeded: 10, DeadLetter: 1, OverdueClaims: 3,
			OldestOutstandingAge: 12500 * time.Millisecond,
		},
		Coordinator: &status.CoordinatorSnapshot{
			CollectorInstances: []status.CollectorInstanceSummary{
				{
					InstanceID: "git-1", CollectorKind: "git", Mode: "direct",
					Enabled: true, ClaimsEnabled: true, DisplayName: "git",
					LastObservedAt: fixed, UpdatedAt: fixed,
				},
			},
		},
		SemanticExtraction: status.SemanticExtractionStatus{
			State: "unavailable", Reason: "provider_not_configured", ProviderConfigured: false,
		},
	}
}

// evidenceBundleParityServer mounts the real query.StatusHandler AND
// query.EvidenceHandler -- sharing one status.Reader and GraphQuery fixture
// -- on a single httptest server, so both the CLI's three-route fetch path
// and a direct GET /api/v0/evidence/bundle call read the identical snapshot.
func evidenceBundleParityServer(t *testing.T) *httptest.Server {
	t.Helper()
	reader := parityStatusReader{snapshot: evidenceBundleParityRawSnapshot()}
	graph := parityGraphQuery{count: 5}

	mux := http.NewServeMux()
	(&query.StatusHandler{StatusReader: reader, Neo4j: graph}).Mount(mux)
	(&query.EvidenceHandler{StatusReader: reader, Neo4j: graph}).Mount(mux)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func TestEvidenceBundleAPIRouteMatchesCLILiveExport(t *testing.T) {
	server := evidenceBundleParityServer(t)

	// CLI side: run the exact code `eshu evidence bundle export --live` runs.
	fixedNow := time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)
	originalNow := evidenceBundleLiveNow
	evidenceBundleLiveNow = func() time.Time { return fixedNow }
	t.Cleanup(func() { evidenceBundleLiveNow = originalNow })

	outPath := filepath.Join(t.TempDir(), "bundle.json")
	cmd := newEvidenceBundleExportCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--live", "--service-url", server.URL, "--out", outPath})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("evidence bundle export --live error = %v", err)
	}
	cliRaw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read CLI bundle: %v", err)
	}
	var cliBundle evidencebundle.Bundle
	if err := json.Unmarshal(cliRaw, &cliBundle); err != nil {
		t.Fatalf("decode CLI bundle: %v\n%s", err, cliRaw)
	}

	// API side: call the route directly.
	resp, err := http.Get(server.URL + "/api/v0/evidence/bundle") //nolint:gosec // test server URL is not user-controlled input
	if err != nil {
		t.Fatalf("GET /api/v0/evidence/bundle error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/v0/evidence/bundle status = %d, want 200", resp.StatusCode)
	}
	var apiBundle evidencebundle.Bundle
	if err := json.NewDecoder(resp.Body).Decode(&apiBundle); err != nil {
		t.Fatalf("decode API bundle: %v", err)
	}

	// Both bundles must independently validate.
	if err := evidencebundle.Validate(cliBundle); err != nil {
		t.Fatalf("CLI bundle failed Validate: %v", err)
	}
	if err := evidencebundle.Validate(apiBundle); err != nil {
		t.Fatalf("API bundle failed Validate: %v", err)
	}

	// Identity.CreatedAt and bundle_id are legitimately different: the CLI and
	// the API each stamp their own fetch time (evidenceBundleLiveNow on the
	// CLI side, time.Now() inside the handler), and bundle_id is a hash over
	// the full content including that timestamp. Blank both before comparing
	// structural equality so the comparison targets composed content, not
	// wall-clock skew between the two calls.
	cliBundle.Identity.CreatedAt = ""
	cliBundle.BundleID = ""
	apiBundle.Identity.CreatedAt = ""
	apiBundle.BundleID = ""

	cliJSON, err := json.MarshalIndent(cliBundle, "", "  ")
	if err != nil {
		t.Fatalf("marshal CLI bundle: %v", err)
	}
	apiJSON, err := json.MarshalIndent(apiBundle, "", "  ")
	if err != nil {
		t.Fatalf("marshal API bundle: %v", err)
	}
	if string(cliJSON) != string(apiJSON) {
		t.Fatalf("CLI and API bundles are not structurally identical (Identity.CreatedAt/BundleID excluded):\nCLI:\n%s\n\nAPI:\n%s", cliJSON, apiJSON)
	}
}
