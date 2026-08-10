// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/evidencebundle"
)

// TestEvidenceBundleExportLiveComposesBundleFromStatusEndpoints proves
// `eshu evidence bundle export --live` fetches the three existing status
// routes (index, pipeline, collectors), composes a valid live bundle from
// them, and never dials the graph/postgres/provider directly -- only the
// documented status API surface.
func TestEvidenceBundleExportLiveComposesBundleFromStatusEndpoints(t *testing.T) {
	var gotPaths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		switch r.URL.Path {
		case "/api/v0/status/index":
			_, _ = w.Write([]byte(`{
				"repository_count": 5,
				"queue_blockages": [{"stage": "reduce", "domain": "aws_relationship", "conflict_domain": "aws", "conflict_key": "k1", "blocked": 3}, {"stage": "parse", "domain": "code", "conflict_domain": "code", "conflict_key": "k2", "blocked": 2}],
				"semantic_extraction": {
					"state": "unavailable",
					"reason": "provider_not_configured",
					"provider_configured": false
				}
			}`))
		case "/api/v0/status/pipeline":
			_, _ = w.Write([]byte(`{
				"health": {"state": "degraded", "reasons": ["queue backlog"]},
				"queue": {"total": 18, "outstanding": 7, "overdue_claims": 3, "oldest_outstanding_age": 12.5, "pending": 4, "in_flight": 2, "retrying": 1, "succeeded": 10, "failed": 0, "dead_letter": 1},
				"generation_history": {"active": 1, "pending": 0, "completed": 9, "superseded": 5, "failed": 0, "other": 2},
				"stage_summaries": [{"stage": "parse", "pending": 2, "claimed": 6, "running": 3, "retrying": 0, "succeeded": 11, "failed": 0, "dead_letter": 0}],
				"domain_backlogs": [{"domain": "aws_relationship_materialization", "outstanding": 1, "in_flight": 4, "blocked": 9, "retrying": 0, "failed": 0, "dead_letter": 1, "oldest_age": 41.5}],
				"scope_activity": {"active": 5, "changed": 1, "unchanged": 4}
			}`))
		case "/api/v0/status/collectors":
			_, _ = w.Write([]byte(`{"collectors": [{"collector_kind": "git", "status_category": "ready", "health": "healthy"}]}`))
		default:
			// t.Fatal* is illegal off the test goroutine: it calls
			// runtime.Goexit and would hang or misreport this request.
			t.Errorf("unexpected request path %s", r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	outPath := filepath.Join(t.TempDir(), "bundle.json")
	cmd := newEvidenceBundleExportCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--live", "--service-url", server.URL, "--out", outPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("evidence bundle export --live error = %v", err)
	}

	wantPaths := map[string]bool{
		"/api/v0/status/index":      false,
		"/api/v0/status/pipeline":   false,
		"/api/v0/status/collectors": false,
	}
	for _, path := range gotPaths {
		wantPaths[path] = true
	}
	for path, seen := range wantPaths {
		if !seen {
			t.Fatalf("request path %s was never fetched; got %v", path, gotPaths)
		}
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	var bundle evidencebundle.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("decode bundle: %v\n%s", err, raw)
	}
	if err := evidencebundle.Validate(bundle); err != nil {
		t.Fatalf("exported live bundle did not validate: %v\n%s", err, raw)
	}
	if bundle.Identity.ScopeID != "live:local" {
		t.Fatalf("bundle.Identity.ScopeID = %q, want the stack-wide default %q", bundle.Identity.ScopeID, "live:local")
	}
	state := bundle.Contents.PipelineState
	if state == nil {
		t.Fatal("bundle.Contents.PipelineState = nil, want populated from the stub status routes")
	}
	if state.RepositoryCount != 5 {
		t.Fatalf("RepositoryCount = %d, want 5", state.RepositoryCount)
	}
	// Every key the stub sends is asserted here. These assertions are what
	// cover the json decode tags: the package-level bundle test populates
	// LiveSnapshot directly and would stay green through a tag typo.
	wantQueue := evidencebundle.PipelineQueueSnapshot{
		Total: 18, Outstanding: 7, OverdueClaims: 3, OldestOutstandingAgeS: 12.5,
		Pending: 4, InFlight: 2, Retrying: 1, Succeeded: 10, DeadLetter: 1,
	}
	if state.Queue != wantQueue {
		t.Fatalf("Queue = %+v, want %+v", state.Queue, wantQueue)
	}
	if state.QueueBlockedCount != 5 {
		t.Fatalf("QueueBlockedCount = %d, want 5 (3+2 gated rows summed across the two blockages)", state.QueueBlockedCount)
	}
	// The status route reports work that is claimed or running separately from
	// what is pending; dropping either bucket makes a busy stage read as idle.
	if len(state.StageSummaries) != 1 {
		t.Fatalf("StageSummaries = %+v, want the one parse stage from the stub", state.StageSummaries)
	}
	wantStage := evidencebundle.PipelineStageSummarySnapshot{
		Stage: "parse", Pending: 2, Claimed: 6, Running: 3, Succeeded: 11,
	}
	if got := state.StageSummaries[0]; got != wantStage {
		t.Fatalf("parse stage = %+v, want %+v from the stub", got, wantStage)
	}
	wantGeneration := evidencebundle.PipelineGenerationHistorySnapshot{
		Active: 1, Completed: 9, Superseded: 5, Other: 2,
	}
	if state.GenerationHistory != wantGeneration {
		t.Fatalf("GenerationHistory = %+v, want %+v from the stub", state.GenerationHistory, wantGeneration)
	}
	wantDomain := evidencebundle.PipelineDomainBacklogSnapshot{
		Domain: "aws_relationship_materialization", Outstanding: 1, InFlight: 4,
		Blocked: 9, DeadLetter: 1, OldestAgeS: 41.5,
	}
	if len(state.DomainBacklogs) != 1 || state.DomainBacklogs[0] != wantDomain {
		t.Fatalf("DomainBacklogs = %+v, want %+v from the stub", state.DomainBacklogs, wantDomain)
	}
	if len(state.Collectors) != 1 || state.Collectors[0].CollectorKind != "git" {
		t.Fatalf("Collectors = %+v, want the git collector from the stub", state.Collectors)
	}
	provider := bundle.Contents.SemanticProviderState
	if provider == nil {
		t.Fatal("bundle.Contents.SemanticProviderState = nil, want populated from the stub status route")
	}
	if provider.ProviderConfigured {
		t.Fatal("ProviderConfigured = true, want false for the stub's no-provider response")
	}
	if provider.State != "unavailable" || provider.Reason != "provider_not_configured" {
		t.Fatalf("state=%q reason=%q, want unavailable/provider_not_configured", provider.State, provider.Reason)
	}
	foundFactCounts := false
	for _, missing := range bundle.Missing {
		if missing.Family == "fact_counts" {
			foundFactCounts = true
		}
	}
	if !foundFactCounts {
		t.Fatal("bundle.Missing does not contain a fact_counts entry")
	}
}

// TestEvidenceBundleExportLiveFailsOnNon200StatusRoute proves a status route
// returning a non-2xx response fails the export instead of silently
// producing a bundle from partial/zero-value data.
func TestEvidenceBundleExportLiveFailsOnNon200StatusRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "status reader not configured", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cmd := newEvidenceBundleExportCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--live", "--service-url", server.URL})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("evidence bundle export --live error = nil, want failure for a non-200 status route")
	}
}

// TestEvidenceBundleExportLiveRejectsRepositoryScope proves --scope is refused
// with --live. The three status routes a live bundle reads are stack-wide, so
// stamping a single repository's ID on them would attribute every other
// repository's queue and collector state to that one repository.
func TestEvidenceBundleExportLiveRejectsRepositoryScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("a rejected scoped live export must not reach the status routes")
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	cmd := newEvidenceBundleExportCommand()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--live", "--scope", "repo:example", "--service-url", server.URL})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("evidence bundle export --live --scope error = nil, want a refusal")
	}
	if !strings.Contains(err.Error(), "--scope cannot be combined with --live") {
		t.Fatalf("error = %v, want it to name the --scope/--live conflict", err)
	}
}

// TestEvidenceCommandTreeExposesTheAdvertisedLiveArgv walks the real command
// tree from the same constructor main wires into rootCmd, so the argv the docs
// and the bundle's own reproduce call advertise -- "eshu evidence bundle export
// --live" -- is checked against the wiring rather than assumed. The sibling
// tests drive the leaf command directly and would still pass if a parent were
// renamed or the leaf never attached.
func TestEvidenceCommandTreeExposesTheAdvertisedLiveArgv(t *testing.T) {
	root := newEvidenceCommand()
	leaf, _, err := root.Find([]string{"bundle", "export"})
	if err != nil {
		t.Fatalf("Find(bundle export) error = %v", err)
	}
	if leaf.Name() != "export" || leaf.Parent().Name() != "bundle" || leaf.Parent().Parent().Name() != "evidence" {
		t.Fatalf("resolved %q under %q/%q, want export under bundle/evidence",
			leaf.Name(), leaf.Parent().Name(), leaf.Parent().Parent().Name())
	}
	if leaf.Flags().Lookup("live") == nil {
		t.Fatal("the advertised argv resolves to a command with no --live flag")
	}
}
