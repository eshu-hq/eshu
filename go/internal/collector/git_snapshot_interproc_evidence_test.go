// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

func interprocFindingFixture() []map[string]any {
	return []map[string]any{{
		// FunctionID format: repo\x1fpkg\x1freceiver\x1fname.
		"source_func": "\x1fpkg\x1f\x1fview",
		"sink_func":   "\x1fpkg\x1f\x1frunQuery",
		"source_kind": "http_request",
		"sink_kind":   "sql",
		"confidence":  0.6,
		"lang":        "go",
	}}
}

func viewAndRunQueryEntities() []content.EntityRecord {
	return []content.EntityRecord{
		{EntityID: "func-view", Path: "src/handler.go", EntityType: "Function", EntityName: "view", StartLine: 3},
		{EntityID: "func-runquery", Path: "src/handler.go", EntityType: "Function", EntityName: "runQuery", StartLine: 9},
	}
}

func TestFunctionIDName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		id           string
		wantReceiver string
		wantName     string
	}{
		{"\x1fpkg\x1fA\x1fHandle", "A", "Handle"},
		{"\x1fpkg\x1frecv\x1fhandle", "recv", "handle"},
		{"\x1f\x1f\x1fview", "", "view"},
		{"bare", "", "bare"},
		{"", "", ""},
	}
	for _, tc := range cases {
		receiver, name := functionIDReceiverName(tc.id)
		if receiver != tc.wantReceiver || name != tc.wantName {
			t.Fatalf("functionIDReceiverName(%q) = (%q, %q), want (%q, %q)", tc.id, receiver, name, tc.wantReceiver, tc.wantName)
		}
	}
}

// TestBuildInterprocTaintEvidencePreservesReceiver proves two methods with the
// same name on different receivers are disambiguated by their class_context, so
// a valid cross-function finding between them is not dropped as ambiguous.
func TestBuildInterprocTaintEvidencePreservesReceiver(t *testing.T) {
	t.Parallel()

	parsed := []map[string]any{{"path": "/repo/src/handler.go", "interproc_findings": []map[string]any{{
		"source_func": "\x1fpkg\x1fA\x1fHandle",
		"sink_func":   "\x1fpkg\x1fB\x1fHandle",
		"source_kind": "http_request",
		"sink_kind":   "sql",
		"confidence":  0.6,
		"lang":        "go",
	}}}}
	entities := []content.EntityRecord{
		{EntityID: "func-a-handle", Path: "src/handler.go", EntityType: "Function", EntityName: "Handle", StartLine: 3, Metadata: map[string]any{"class_context": "A"}},
		{EntityID: "func-b-handle", Path: "src/handler.go", EntityType: "Function", EntityName: "Handle", StartLine: 9, Metadata: map[string]any{"class_context": "B"}},
	}
	evidence := buildInterprocTaintEvidence("/repo", parsed, newFunctionUIDResolver(entities))
	if len(evidence) != 1 {
		t.Fatalf("same-named methods on distinct receivers must resolve, got %d: %+v", len(evidence), evidence)
	}
	if evidence[0].SourceFunctionUID != "func-a-handle" || evidence[0].SinkFunctionUID != "func-b-handle" {
		t.Fatalf("receiver-disambiguated endpoints mis-resolved: %+v", evidence[0])
	}
}

// TestBuildInterprocTaintEvidenceResolvesBothEndpoints proves a cross-function
// finding resolves both source and sink functions to their entity uids.
func TestBuildInterprocTaintEvidenceResolvesBothEndpoints(t *testing.T) {
	t.Parallel()

	parsed := []map[string]any{{"path": "/repo/src/handler.go", "interproc_findings": interprocFindingFixture()}}
	evidence := buildInterprocTaintEvidence("/repo", parsed, newFunctionUIDResolver(viewAndRunQueryEntities()))
	if len(evidence) != 1 {
		t.Fatalf("want 1 evidence row, got %d: %+v", len(evidence), evidence)
	}
	got := evidence[0]
	if got.SourceFunctionUID != "func-view" || got.SinkFunctionUID != "func-runquery" {
		t.Fatalf("endpoints not resolved: %+v", got)
	}
	if got.SinkKind != "sql" || got.SourceKind != "http_request" || got.RelativePath != "src/handler.go" {
		t.Fatalf("fields not mapped: %+v", got)
	}
}

// TestBuildInterprocTaintEvidencePreservesWhyTrail proves ordered parser trail
// steps are resolved to Function uids where possible and kept as derived
// evidence payload, not graph truth.
func TestBuildInterprocTaintEvidencePreservesWhyTrail(t *testing.T) {
	t.Parallel()

	parsed := []map[string]any{{"path": "/repo/src/handler.go", "interproc_findings": []map[string]any{{
		"source_func": "\x1fpkg\x1f\x1fview",
		"sink_func":   "\x1fpkg\x1f\x1frunQuery",
		"source_kind": "http_request",
		"sink_kind":   "sql",
		"confidence":  0.6,
		"lang":        "go",
		"why_trail": []map[string]any{
			{"function_id": "\x1fpkg\x1f\x1fview", "slot_kind": "param", "slot_index": 0},
			{"function_id": "\x1fpkg\x1f\x1fservice", "slot_kind": "named", "slot_name": "payload"},
			{"function_id": "\x1fpkg\x1f\x1frunQuery", "slot_kind": "return"},
		},
		"why_trail_truncated": true,
	}}}}
	entities := append(viewAndRunQueryEntities(), content.EntityRecord{
		EntityID: "func-service", Path: "src/handler.go", EntityType: "Function", EntityName: "service", StartLine: 6,
	})

	evidence := buildInterprocTaintEvidence("/repo", parsed, newFunctionUIDResolver(entities))
	if len(evidence) != 1 {
		t.Fatalf("want 1 evidence row, got %d: %+v", len(evidence), evidence)
	}
	trail := evidence[0].WhyTrail
	if len(trail) != 3 {
		t.Fatalf("len(WhyTrail) = %d, want 3: %+v", len(trail), trail)
	}
	if trail[0]["role"] != "source" || trail[0]["function_uid"] != "func-view" {
		t.Fatalf("source step not resolved/role-stamped: %+v", trail[0])
	}
	if trail[1]["role"] != "intermediate" || trail[1]["function_uid"] != "func-service" || trail[1]["slot_name"] != "payload" {
		t.Fatalf("intermediate step not resolved/role-stamped: %+v", trail[1])
	}
	if trail[2]["role"] != "sink" || trail[2]["function_uid"] != "func-runquery" {
		t.Fatalf("sink step not resolved/role-stamped: %+v", trail[2])
	}
	if !evidence[0].WhyTrailTruncated {
		t.Fatalf("WhyTrailTruncated = false, want true")
	}
}

// TestBuildInterprocTaintEvidenceDropsAmbiguousName proves a finding is dropped
// when an endpoint name is not unique within the file (cannot attribute safely).
func TestBuildInterprocTaintEvidenceDropsAmbiguousName(t *testing.T) {
	t.Parallel()

	parsed := []map[string]any{{"path": "/repo/src/handler.go", "interproc_findings": interprocFindingFixture()}}
	entities := append(viewAndRunQueryEntities(), content.EntityRecord{
		EntityID: "func-runquery-2", Path: "src/handler.go", EntityType: "Function", EntityName: "runQuery", StartLine: 20,
	})
	if evidence := buildInterprocTaintEvidence("/repo", parsed, newFunctionUIDResolver(entities)); len(evidence) != 0 {
		t.Fatalf("ambiguous sink name must drop the finding, got %+v", evidence)
	}
}

// TestBuildInterprocTaintEvidenceDropsUnresolved proves a finding is dropped when
// an endpoint does not materialize as an entity.
func TestBuildInterprocTaintEvidenceDropsUnresolved(t *testing.T) {
	t.Parallel()

	parsed := []map[string]any{{"path": "/repo/src/handler.go", "interproc_findings": interprocFindingFixture()}}
	// Only the source function exists; the sink does not.
	entities := []content.EntityRecord{{EntityID: "func-view", Path: "src/handler.go", EntityType: "Function", EntityName: "view", StartLine: 3}}
	if evidence := buildInterprocTaintEvidence("/repo", parsed, newFunctionUIDResolver(entities)); len(evidence) != 0 {
		t.Fatalf("unresolved sink must drop the finding, got %+v", evidence)
	}
}

// TestBuildInterprocTaintEvidenceEmptyWithoutFindings proves the result is empty
// (byte-identical when off) when there are no interproc findings.
func TestBuildInterprocTaintEvidenceEmptyWithoutFindings(t *testing.T) {
	t.Parallel()

	parsed := []map[string]any{{"path": "/repo/src/handler.go"}}
	if evidence := buildInterprocTaintEvidence("/repo", parsed, newFunctionUIDResolver(viewAndRunQueryEntities())); evidence != nil {
		t.Fatalf("no findings must yield nil evidence, got %+v", evidence)
	}
}

// TestInterprocEvidenceFactEnvelope proves the fact kind, payload, idempotent key,
// and the cloud flag.
func TestInterprocEvidenceFactEnvelope(t *testing.T) {
	t.Parallel()

	evidence := InterprocTaintEvidenceSnapshot{
		SourceFunctionUID: "func-view", SinkFunctionUID: "func-runquery",
		RelativePath: "src/handler.go", SourceFunctionName: "view", SinkFunctionName: "runQuery",
		Language: "go", SinkKind: "sql", SourceKind: "http_request", Confidence: 0.6, Cloud: true,
	}
	at := time.Date(2026, 6, 18, 0, 0, 0, 0, time.UTC)
	env := interprocEvidenceFactEnvelope("/repo", "repo-1", "scope-1", "gen-1", at, evidence)
	if env.FactKind != "code_interproc_evidence" {
		t.Fatalf("FactKind = %q, want code_interproc_evidence", env.FactKind)
	}
	if env.Payload["source_function_uid"] != "func-view" || env.Payload["sink_function_uid"] != "func-runquery" || env.Payload["cloud"] != true {
		t.Fatalf("payload not mapped: %+v", env.Payload)
	}
	if env.Payload["why_trail"] != nil {
		t.Fatalf("empty why_trail should be omitted: %+v", env.Payload)
	}
	again := interprocEvidenceFactEnvelope("/repo", "repo-1", "scope-1", "gen-1", at, evidence)
	if env.StableFactKey != again.StableFactKey {
		t.Fatalf("fact key not stable across re-emission")
	}
}

func TestInterprocEvidenceFactEnvelopeCarriesWhyTrail(t *testing.T) {
	t.Parallel()

	evidence := InterprocTaintEvidenceSnapshot{
		SourceFunctionUID: "func-view", SinkFunctionUID: "func-runquery",
		RelativePath: "src/handler.go", SourceFunctionName: "view", SinkFunctionName: "runQuery",
		Language: "go", SinkKind: "sql", SourceKind: "http_request", Confidence: 0.6,
		WhyTrail:          []map[string]any{{"role": "source", "function_uid": "func-view"}},
		WhyTrailTruncated: true,
	}
	env := interprocEvidenceFactEnvelope("/repo", "repo-1", "scope-1", "gen-1", time.Now(), evidence)
	trail, ok := env.Payload["why_trail"].([]map[string]any)
	if !ok || len(trail) != 1 || trail[0]["function_uid"] != "func-view" {
		t.Fatalf("why_trail not carried: %+v", env.Payload)
	}
	if env.Payload["why_trail_truncated"] != true {
		t.Fatalf("why_trail_truncated not carried: %+v", env.Payload)
	}
}

// TestInterprocEvidenceCountedStreamedAndFreshness proves interproc evidence is
// counted in FactCount, streamed as a code_interproc_evidence fact, and changes
// the freshness hint.
func TestInterprocEvidenceCountedStreamedAndFreshness(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	observedAt := time.Date(2026, time.June, 18, 0, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)

	base := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	withEvidence := testCollectorSnapshot(repoPath, "package main\n", "digest-1")
	withEvidence.InterprocTaintEvidence = []InterprocTaintEvidenceSnapshot{{
		SourceFunctionUID: "func-view", SinkFunctionUID: "func-runquery", RelativePath: "main.go",
		SourceFunctionName: "view", SinkFunctionName: "runQuery", Language: "go",
		SinkKind: "sql", SourceKind: "http_request", Confidence: 0.6,
	}}

	baseCollected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, base, false)
	baseFacts := drainFactChannel(baseCollected.Facts)

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, withEvidence, false)
	envelopes := drainFactChannel(collected.Facts)

	if got, want := len(envelopes), collected.FactCount; got != want {
		t.Fatalf("streamed facts = %d, FactCount = %d (interproc evidence not counted)", got, want)
	}
	if got := len(envelopes) - len(baseFacts); got != 1 {
		t.Fatalf("interproc evidence added %d facts, want 1", got)
	}
	found := false
	for _, e := range envelopes {
		if e.FactKind == "code_interproc_evidence" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no code_interproc_evidence fact emitted")
	}
	if baseCollected.Generation.FreshnessHint == collected.Generation.FreshnessHint {
		t.Fatalf("freshness hint unchanged despite interproc evidence")
	}
}
