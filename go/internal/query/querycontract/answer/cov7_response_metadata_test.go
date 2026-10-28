// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answer

import (
	"testing"
)

// TestCov7AttachAnswerMetadataNilKeepsNil proves the nil guard: a nil payload
// stays nil instead of gaining an answer_metadata entry.
func TestCov7AttachAnswerMetadataNilKeepsNil(t *testing.T) {
	t.Parallel()

	if got := AttachAnswerMetadata(nil); got != nil {
		t.Fatalf("AttachAnswerMetadata(nil) = %#v, want nil", got)
	}
}

// TestCov7AttachAnswerMetadataStampsCompanion proves a non-nil payload gains
// an answer_metadata entry carrying the schema version.
func TestCov7AttachAnswerMetadataStampsCompanion(t *testing.T) {
	t.Parallel()

	data := map[string]any{"truncated": false}
	got := AttachAnswerMetadata(data)
	metadata, ok := AnswerMetadataFromData(got)
	if !ok {
		t.Fatalf("AnswerMetadataFromData = not ok, want attached companion")
	}
	if metadata.SchemaVersion != AnswerMetadataSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", metadata.SchemaVersion, AnswerMetadataSchemaVersion)
	}
}

// TestCov7BuildAnswerMetadataTruncationSignals proves every truncation marker
// (route, coverage, pool, and both deadline levels) flips Truncated and feeds
// the partial reasons.
func TestCov7BuildAnswerMetadataTruncationSignals(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		data map[string]any
	}{
		{"route truncated", map[string]any{"truncated": true}},
		{"coverage truncated", map[string]any{"coverage": map[string]any{"truncated": true}}},
		{"pool truncated", map[string]any{"coverage": map[string]any{"candidate_pool_truncated": true}}},
		{"route deadline", map[string]any{"deadline_truncated": true}},
		{"coverage deadline", map[string]any{"coverage": map[string]any{"deadline_truncated": true}}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			metadata := BuildAnswerMetadata(tc.data)
			if !metadata.Truncated {
				t.Fatalf("Truncated = false, want true for %#v", tc.data)
			}
			found := false
			for _, reason := range metadata.PartialReasons {
				if reason == "result_truncated" {
					found = true
				}
			}
			if !found {
				t.Fatalf("PartialReasons = %#v, want result_truncated", metadata.PartialReasons)
			}
		})
	}

	quiet := BuildAnswerMetadata(map[string]any{})
	if quiet.Truncated {
		t.Fatalf("Truncated = true for empty payload, want false")
	}
}

// TestCov7BuildAnswerMetadataEvidenceHandles proves handles merge across the
// evidence_groups, matched_files, code_surface, and direct/transitive impact
// shapes with sorted dedup.
func TestCov7BuildAnswerMetadataEvidenceHandles(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"evidence_groups": []any{
			map[string]any{"source_handle": map[string]any{"kind": "file", "repo_id": "r1", "relative_path": "a.go"}},
			map[string]any{"source_handle": map[string]any{"kind": "file", "repo_id": "r1", "relative_path": "a.go"}},
		},
		"matched_files": []any{
			map[string]any{"source_handle": map[string]any{"repo_id": "r2", "relative_path": "b.go"}},
		},
		"code_surface": map[string]any{
			"changed_files": []any{
				map[string]any{"source_handle": map[string]any{"entity_id": "e1"}},
			},
		},
		"direct_impact": []any{
			map[string]any{"evidence_handle": map[string]any{"kind": "entity", "entity_id": "e2"}},
		},
		"transitive_impact": []any{
			map[string]any{"evidence_handle": map[string]any{"kind": "ghost"}},
		},
		"call_graph_handles": []any{
			map[string]any{"kind": "file", "repo_id": "r3", "relative_path": "c.go"},
		},
		"evidence_handles": []any{
			map[string]any{"kind": "file", "repo_id": "r0", "relative_path": "z.go"},
		},
	}
	metadata := BuildAnswerMetadata(data)
	// Seven candidate rows collapse: the duplicate a.go handle dedups and the
	// ghost transitive row (no identity) drops, leaving six.
	if len(metadata.EvidenceHandles) != 6 {
		t.Fatalf("EvidenceHandles = %#v, want 6 merged rows", metadata.EvidenceHandles)
	}
	if metadata.EvidenceHandles[0]["entity_id"] != "e1" {
		t.Fatalf("first handle = %#v, want sorted entity row first", metadata.EvidenceHandles[0])
	}
}

// TestCov7NormalizeMetadataHandleKindInference proves kind inference for
// entity and file rows, line clamping, and the nil contract for identity-free
// rows.
func TestCov7NormalizeMetadataHandleKindInference(t *testing.T) {
	t.Parallel()

	entity := normalizeMetadataHandle(map[string]any{"entity_id": "e1"})
	if entity["kind"] != "entity" {
		t.Fatalf("entity handle = %#v, want inferred kind entity", entity)
	}
	file := normalizeMetadataHandle(map[string]any{"repo_id": "r1", "relative_path": "a.go", "start_line": 3, "end_line": 0})
	if file["kind"] != "file" {
		t.Fatalf("file handle = %#v, want inferred kind file", file)
	}
	if _, ok := file["end_line"]; ok {
		t.Fatalf("file handle = %#v, want non-positive end_line dropped", file)
	}
	if got := normalizeMetadataHandle(map[string]any{"reason": "orphan"}); got != nil {
		t.Fatalf("identity-free handle = %#v, want nil", got)
	}
	if got := normalizeMetadataHandle(map[string]any{"repo_id": "r1"}); got != nil {
		t.Fatalf("repo-only handle = %#v, want nil without relative_path", got)
	}
}

// TestCov7BuildAnswerMetadataMissingAndLimits proves missing-evidence rows
// (top-level, nested overviews, string shorthand) and limitations (service
// identity, truncation caveat) merge with dedup.
func TestCov7BuildAnswerMetadataMissingAndLimits(t *testing.T) {
	t.Parallel()

	data := map[string]any{
		"missing_evidence": []any{"stale fuji snapshot", map[string]any{"kind": "repo", "reason": "no grant"}},
		"support_overview": map[string]any{
			"target_support": map[string]any{"missing_evidence": []any{map[string]any{"explanation": "no support row"}}},
		},
		"documentation_overview": map[string]any{},
		"service_identity":       map[string]any{"limitations": []any{map[string]any{"kind": "scope", "reason": "bounded"}}},
		"limitations":            []any{map[string]any{"message": "slow path"}},
		"truncated":              true,
		"recommended_next_calls": []any{map[string]any{"tool": "t", "route": "r", "reason": "why"}},
	}
	metadata := BuildAnswerMetadata(data)
	if len(metadata.MissingEvidence) != 3 {
		t.Fatalf("MissingEvidence = %#v, want 3 rows", metadata.MissingEvidence)
	}
	if len(metadata.Limitations) != 3 {
		t.Fatalf("Limitations = %#v, want service + message + truncation rows", metadata.Limitations)
	}
	if len(metadata.RecommendedNextCalls) != 1 {
		t.Fatalf("RecommendedNextCalls = %#v, want 1 row", metadata.RecommendedNextCalls)
	}
	found := false
	for _, reason := range metadata.PartialReasons {
		if reason == "missing_evidence" {
			found = true
		}
	}
	if !found {
		t.Fatalf("PartialReasons = %#v, want missing_evidence", metadata.PartialReasons)
	}
}

// TestCov7BuildAnswerMetadataCoverageStateReasons proves a non-terminal
// coverage state adds a coverage_<state> reason while terminal states stay
// quiet.
func TestCov7BuildAnswerMetadataCoverageStateReasons(t *testing.T) {
	t.Parallel()

	stale := BuildAnswerMetadata(map[string]any{"coverage_summary": map[string]any{"state": "stale"}})
	found := false
	for _, reason := range stale.PartialReasons {
		if reason == "coverage_stale" {
			found = true
		}
	}
	if !found {
		t.Fatalf("PartialReasons = %#v, want coverage_stale", stale.PartialReasons)
	}
	for _, state := range []string{"complete", "exact", "fresh", "supported"} {
		metadata := BuildAnswerMetadata(map[string]any{"result_limits": map[string]any{"state": state}})
		for _, reason := range metadata.PartialReasons {
			if reason == "coverage_"+state {
				t.Fatalf("PartialReasons = %#v, want no reason for terminal state %q", metadata.PartialReasons, state)
			}
		}
	}
	if coverage := answerMetadataCoverage(map[string]any{}); len(coverage) != 0 {
		t.Fatalf("answerMetadataCoverage({}) = %#v, want empty", coverage)
	}
}

// TestCov7AnswerMetadataFromDataShapes proves extraction across the struct,
// map, nil, and foreign-type shapes.
func TestCov7AnswerMetadataFromDataShapes(t *testing.T) {
	t.Parallel()

	if _, ok := AnswerMetadataFromData(nil); ok {
		t.Fatalf("AnswerMetadataFromData(nil) = ok, want false")
	}
	built := AnswerMetadata{SchemaVersion: AnswerMetadataSchemaVersion}
	roundTripped, ok := AnswerMetadataFromData(map[string]any{"answer_metadata": built})
	if !ok || roundTripped.SchemaVersion != AnswerMetadataSchemaVersion {
		t.Fatalf("struct round trip = (%#v, %v), want ok with schema version", roundTripped, ok)
	}
	decoded, ok := AnswerMetadataFromData(map[string]any{"answer_metadata": map[string]any{
		"schema_version":         AnswerMetadataSchemaVersion,
		"evidence_handles":       map[string]any{"kind": "file"},
		"missing_evidence":       []string{"gone"},
		"limitations":            "slow",
		"truncated":              true,
		"coverage":               map[string]any{"state": "stale"},
		"partial_reasons":        []any{"result_truncated"},
		"recommended_next_calls": []map[string]any{{"tool": "t"}},
	}})
	if !ok || len(decoded.EvidenceHandles) != 1 || len(decoded.MissingEvidence) != 1 || !decoded.Truncated {
		t.Fatalf("map decode = (%#v, %v), want ok with merged rows", decoded, ok)
	}
	if _, ok := AnswerMetadataFromData(map[string]any{"answer_metadata": map[string]any{}}); ok {
		t.Fatalf("schemaless map = ok, want false")
	}
	if _, ok := AnswerMetadataFromData(map[string]any{"answer_metadata": 42}); ok {
		t.Fatalf("foreign type = ok, want false")
	}
	if _, ok := AnswerMetadataFromData(map[string]any{}); ok {
		t.Fatalf("absent companion = ok, want false")
	}
}

// TestCov7WithDefaultsFillsEverySlot proves every nil slot normalizes to an
// empty container and the schema version fills in.
func TestCov7WithDefaultsFillsEverySlot(t *testing.T) {
	t.Parallel()

	got := AnswerMetadata{}.WithDefaults()
	if got.SchemaVersion != AnswerMetadataSchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", got.SchemaVersion, AnswerMetadataSchemaVersion)
	}
	if got.EvidenceHandles == nil || got.MissingEvidence == nil || got.Limitations == nil ||
		got.Coverage == nil || got.PartialReasons == nil || got.RecommendedNextCalls == nil {
		t.Fatalf("WithDefaults = %#v, want no nil slots", got)
	}
	kept := AnswerMetadata{SchemaVersion: "custom", Coverage: map[string]any{"a": 1}}.WithDefaults()
	if kept.SchemaVersion != "custom" || len(kept.Coverage) != 1 {
		t.Fatalf("WithDefaults overwrote set slots: %#v", kept)
	}
}

// TestCov7MetadataMapRowsShapes proves every metadataMapRows input shape,
// including blank-string filtering and foreign-type stringification.
func TestCov7MetadataMapRowsShapes(t *testing.T) {
	t.Parallel()

	if rows := metadataMapRows(nil); len(rows) != 0 {
		t.Fatalf("nil rows = %#v, want empty", rows)
	}
	if rows := metadataMapRows(map[string]any{"a": 1}); len(rows) != 1 {
		t.Fatalf("map rows = %#v, want 1", rows)
	}
	rows := metadataMapRows([]any{
		map[string]any{"a": 1},
		"  spaced  ",
		"   ",
		42,
	})
	// Only the map and the blank-trimmed string survive: a foreign scalar
	// inside a list has no case and drops, unlike a top-level scalar.
	if len(rows) != 2 || rows[1]["reason"] != "spaced" {
		t.Fatalf("mixed rows = %#v, want map + string rows", rows)
	}
	strings := metadataMapRows([]string{"keep", "  "})
	if len(strings) != 1 {
		t.Fatalf("string rows = %#v, want blank dropped", strings)
	}
	if rows := metadataMapRows(7); len(rows) != 1 || rows[0]["reason"] != "7" {
		t.Fatalf("scalar rows = %#v, want stringified row", rows)
	}
	nested := nestedReasonRows(map[string]any{"a": map[string]any{"b": map[string]any{}}}, "a", "missing", "rows")
	if nested != nil {
		t.Fatalf("nested missing rows = %#v, want nil", nested)
	}
}

// TestCov7MetadataRowDedupAndReasons proves dedup sorts and drops empty-key
// rows, and appendUniqueReason ignores blanks and duplicates.
func TestCov7MetadataRowDedupAndReasons(t *testing.T) {
	t.Parallel()

	if rows := dedupeMetadataRows(nil, "kind"); rows == nil || len(rows) != 0 {
		t.Fatalf("nil dedup = %#v, want empty non-nil", rows)
	}
	rows := dedupeMetadataRows([]map[string]any{
		{"kind": "b", "reason": "2"},
		{"kind": "a", "reason": "1"},
		{"kind": "a", "reason": "1"},
		{"kind": "  | ", "reason": "  "},
	}, "kind", "reason")
	if len(rows) != 2 || rows[0]["kind"] != "a" || rows[1]["kind"] != "b" {
		t.Fatalf("deduped rows = %#v, want sorted unique pair", rows)
	}
	// A missing key stringifies to <nil>, which survives the pipe trim: only
	// all-blank values collapse to the empty key.
	if key := metadataRowKey(map[string]any{}, "kind"); key != "<nil>" {
		t.Fatalf("missing-key row key = %q, want <nil>", key)
	}
	if key := metadataRowKey(map[string]any{"kind": "  | "}, "kind"); key != "" {
		t.Fatalf("blank row key = %q, want empty", key)
	}
	reasons := appendUniqueReason(appendUniqueReason([]string{"a"}, "a"), "  ")
	reasons = appendUniqueReason(reasons, "b")
	if len(reasons) != 2 || reasons[1] != "b" {
		t.Fatalf("reasons = %#v, want [a b]", reasons)
	}
	derived := metadataReasonRows([]any{map[string]any{}, map[string]any{"slot": "s1"}})
	if len(derived) != 1 || derived[0]["reason"] != "s1" {
		t.Fatalf("reason rows = %#v, want empty skipped and slot-derived reason", derived)
	}
}
