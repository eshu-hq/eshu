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
	cases := map[string]string{
		"\x1fpkg\x1frecv\x1fhandle": "handle",
		"\x1f\x1f\x1fview":          "view",
		"bare":                      "bare",
		"":                          "",
	}
	for id, want := range cases {
		if got := functionIDName(id); got != want {
			t.Fatalf("functionIDName(%q) = %q, want %q", id, got, want)
		}
	}
}

// TestBuildInterprocTaintEvidenceResolvesBothEndpoints proves a cross-function
// finding resolves both source and sink functions to their entity uids.
func TestBuildInterprocTaintEvidenceResolvesBothEndpoints(t *testing.T) {
	t.Parallel()

	parsed := []map[string]any{{"path": "/repo/src/handler.go", "interproc_findings": interprocFindingFixture()}}
	evidence := buildInterprocTaintEvidence("/repo", parsed, viewAndRunQueryEntities())
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

// TestBuildInterprocTaintEvidenceDropsAmbiguousName proves a finding is dropped
// when an endpoint name is not unique within the file (cannot attribute safely).
func TestBuildInterprocTaintEvidenceDropsAmbiguousName(t *testing.T) {
	t.Parallel()

	parsed := []map[string]any{{"path": "/repo/src/handler.go", "interproc_findings": interprocFindingFixture()}}
	entities := append(viewAndRunQueryEntities(), content.EntityRecord{
		EntityID: "func-runquery-2", Path: "src/handler.go", EntityType: "Function", EntityName: "runQuery", StartLine: 20,
	})
	if evidence := buildInterprocTaintEvidence("/repo", parsed, entities); len(evidence) != 0 {
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
	if evidence := buildInterprocTaintEvidence("/repo", parsed, entities); len(evidence) != 0 {
		t.Fatalf("unresolved sink must drop the finding, got %+v", evidence)
	}
}

// TestBuildInterprocTaintEvidenceEmptyWithoutFindings proves the result is empty
// (byte-identical when off) when there are no interproc findings.
func TestBuildInterprocTaintEvidenceEmptyWithoutFindings(t *testing.T) {
	t.Parallel()

	parsed := []map[string]any{{"path": "/repo/src/handler.go"}}
	if evidence := buildInterprocTaintEvidence("/repo", parsed, viewAndRunQueryEntities()); evidence != nil {
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
	again := interprocEvidenceFactEnvelope("/repo", "repo-1", "scope-1", "gen-1", at, evidence)
	if env.StableFactKey != again.StableFactKey {
		t.Fatalf("fact key not stable across re-emission")
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
