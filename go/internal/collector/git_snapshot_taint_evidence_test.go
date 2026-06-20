package collector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

func taintFindingFixture() []map[string]any {
	return []map[string]any{{
		"function_name": "handle",
		"line_number":   3,
		"lang":          "go",
		"kind":          "TAINTED",
		"sink_kind":     "sql",
		"source_kind":   "http_request",
		"binding":       "q",
		"source_line":   4,
		"sink_line":     5,
		"confidence":    0.8,
		"guard_reason":  "allowed",
	}}
}

func handleFunctionEntity() []content.EntityRecord {
	return []content.EntityRecord{{
		EntityID:   "func-handle",
		Path:       "src/handler.go",
		EntityType: "Function",
		EntityName: "handle",
		StartLine:  3,
	}}
}

// TestBuildTaintEvidenceResolvesFunctionUID proves a taint finding is resolved to
// its Function entity's uid via the (path, label, name, line) identity.
func TestBuildTaintEvidenceResolvesFunctionUID(t *testing.T) {
	t.Parallel()

	parsedFiles := []map[string]any{{
		"path":           "/repo/src/handler.go",
		"taint_findings": taintFindingFixture(),
	}}
	evidence := buildTaintEvidence("/repo", parsedFiles, handleFunctionEntity())

	if len(evidence) != 1 {
		t.Fatalf("want 1 evidence row, got %d: %+v", len(evidence), evidence)
	}
	got := evidence[0]
	if got.FunctionUID != "func-handle" {
		t.Fatalf("FunctionUID = %q, want func-handle", got.FunctionUID)
	}
	if got.Kind != "TAINTED" || got.SinkKind != "sql" || got.SourceKind != "http_request" || got.Binding != "q" {
		t.Fatalf("finding fields not mapped: %+v", got)
	}
	if got.RelativePath != "src/handler.go" || got.SinkLine != 5 || got.Confidence != 0.8 {
		t.Fatalf("provenance not mapped: %+v", got)
	}
	if got.GuardReason != "allowed" {
		t.Fatalf("guard reason not mapped: %+v", got)
	}
}

// TestBuildTaintEvidenceDropsUnresolved proves a finding whose function does not
// materialize as an entity is dropped (no orphan evidence).
func TestBuildTaintEvidenceDropsUnresolved(t *testing.T) {
	t.Parallel()

	parsedFiles := []map[string]any{{
		"path":           "/repo/src/handler.go",
		"taint_findings": taintFindingFixture(),
	}}
	// No matching entity (different name).
	entities := []content.EntityRecord{{
		EntityID: "func-other", Path: "src/handler.go", EntityType: "Function",
		EntityName: "other", StartLine: 3,
	}}
	if evidence := buildTaintEvidence("/repo", parsedFiles, entities); len(evidence) != 0 {
		t.Fatalf("unresolved finding must be dropped, got %+v", evidence)
	}
}

// TestBuildTaintEvidenceEmptyWithoutFindings proves the result is empty when the
// parser emitted no taint findings, so the snapshot is byte-identical when off.
func TestBuildTaintEvidenceEmptyWithoutFindings(t *testing.T) {
	t.Parallel()

	parsedFiles := []map[string]any{{"path": "/repo/src/handler.go"}}
	if evidence := buildTaintEvidence("/repo", parsedFiles, handleFunctionEntity()); evidence != nil {
		t.Fatalf("no findings must yield nil evidence, got %+v", evidence)
	}
}

// TestTaintEvidenceFactEnvelope proves the fact kind, payload, and an idempotent
// stable key keyed per finding within a function.
func TestTaintEvidenceFactEnvelope(t *testing.T) {
	t.Parallel()

	evidence := TaintEvidenceSnapshot{
		FunctionUID: "func-handle", RelativePath: "src/handler.go", FunctionName: "handle",
		Language: "go", Kind: "TAINTED", SinkKind: "sql", SourceKind: "http_request",
		Binding: "q", SourceLine: 4, SinkLine: 5, Confidence: 0.8,
	}
	at := time.Date(2026, 6, 17, 0, 0, 0, 0, time.UTC)
	env := taintEvidenceFactEnvelope("/repo", "repo-1", "scope-1", "gen-1", at, evidence)

	if env.FactKind != "code_taint_evidence" {
		t.Fatalf("FactKind = %q, want code_taint_evidence", env.FactKind)
	}
	if env.Payload["function_uid"] != "func-handle" || env.Payload["sink_kind"] != "sql" {
		t.Fatalf("payload not mapped: %+v", env.Payload)
	}
	if _, present := env.Payload["guard_reason"]; present {
		t.Fatalf("empty guard reason should be omitted: %+v", env.Payload)
	}
	withGuard := evidence
	withGuard.GuardReason = "allowed"
	if got := taintEvidenceFactEnvelope("/repo", "repo-1", "scope-1", "gen-1", at, withGuard).Payload["guard_reason"]; got != "allowed" {
		t.Fatalf("guard reason not carried in fact payload: %v", got)
	}
	// Re-emitting the same finding yields the same stable key (idempotent).
	again := taintEvidenceFactEnvelope("/repo", "repo-1", "scope-1", "gen-1", at, evidence)
	if env.StableFactKey != again.StableFactKey || env.FactID != again.FactID {
		t.Fatalf("fact key not stable across re-emission")
	}
	// A different finding (different sink line) yields a different key.
	other := evidence
	other.SinkLine = 9
	if taintEvidenceFactEnvelope("/repo", "repo-1", "scope-1", "gen-1", at, other).StableFactKey == env.StableFactKey {
		t.Fatalf("distinct findings must have distinct keys")
	}
}
