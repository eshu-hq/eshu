// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/code"
)

func TestCov6DivergenceColumnAndNilReader(t *testing.T) {
	t.Parallel()
	if got := divergenceColumn(codedivergence.KindRenamed); got != "fp_renamed" {
		t.Fatalf("renamed column = %q, want fp_renamed", got)
	}
	if got := divergenceColumn(codedivergence.KindExact); got != "fp_exact" {
		t.Fatalf("exact column = %q, want fp_exact", got)
	}
	var nilReader *ContentReader
	if stats, err := nilReader.DivergenceGroupStats(context.Background(), "r", codedivergence.KindExact, 2); stats != nil || err != nil {
		t.Fatalf("nil reader stats = (%v,%v), want (nil,nil)", stats, err)
	}
	if members, err := nilReader.DivergenceMembersByEntityID(context.Background(), "r", []string{"e1"}); err != nil || len(members) != 0 {
		t.Fatalf("nil reader members = (%v,%v), want empty", members, err)
	}
	if rows, err := nilReader.DriftedFindingRows(context.Background(), "r", []string{"f1"}); err != nil || len(rows) != 0 {
		t.Fatalf("nil reader drifted rows = (%v,%v), want empty", rows, err)
	}
}

func TestCov6CompatSupplyChainSeams(t *testing.T) {
	t.Parallel()
	if got := uniqueSortedNonEmpty([]string{" b ", "", "a", "b"}); len(got) != 2 || got[0] != "a" {
		t.Fatalf("unique sorted = %v, want [a b]", got)
	}
	summaries, count, truncated := boundedSBOMWarningSummaries([]string{"w1", "w2"})
	if len(summaries) != 2 || count != 2 || truncated {
		t.Fatalf("warnings = (%v,%d,%v), want unbounded passthrough", summaries, count, truncated)
	}
	if !SupplyChainImpactWinnersReadEnabled("true") || SupplyChainImpactWinnersReadEnabled("bogus") {
		t.Fatal("winners read flag mismatch")
	}
	if got := supplyChainCloudRuntimeProbePerDigestLimit(0); got <= 0 {
		t.Fatalf("probe limit = %d, want positive", got)
	}
}

func TestCov6DecodeContainerImageIdentityRow(t *testing.T) {
	t.Parallel()
	row, err := decodeContainerImageIdentityRow("f1", "observed", []byte(`{"digest":"sha256:abc","image_ref":"img:latest"}`))
	if err != nil {
		t.Fatalf("decode row: %v", err)
	}
	if row.IdentityID != "f1" || row.Digest != "sha256:abc" || row.SourceFreshness != "active" || row.SourceConfidence != "observed" {
		t.Fatalf("decoded row = %+v, want identity fields", row)
	}
	if _, err := decodeContainerImageIdentityRow("f1", "observed", []byte("bogus")); err == nil {
		t.Fatal("bogus payload must fail decode")
	}
}

func TestCov6CodeSeamForwarders(t *testing.T) {
	t.Parallel()
	row := CodeTopicEvidenceRow{
		SourceKind: "file", RepoID: "r", RelativePath: "a.go",
		EntityID: "e1", EntityName: "run", EntityType: "Function",
		Language: "go", StartLine: 1, EndLine: 5,
		MatchedTerms: []string{"run"}, Score: 3,
	}
	files := AppendMatchedFile(nil, row)
	if len(files) != 1 || files[0]["relative_path"] != "a.go" {
		t.Fatalf("matched files = %v, want the row's file", files)
	}
	if again := AppendMatchedFile(files, row); len(again) != 1 {
		t.Fatal("duplicate file must not append")
	}
	cypher, params := CallGraphMetricsEdgesCypher("r")
	if !strings.Contains(cypher, "CALLS") || params["repo_id"] != "r" {
		t.Fatalf("edges cypher = (%q,%v), want CALLS with repo param", cypher, params)
	}
	group := CodeTopicEvidenceGroup(row, 1)
	if group["rank"] != 1 || group["entity_id"] != "e1" {
		t.Fatalf("evidence group = %v, want rank and entity", group)
	}
	symbol := CodeTopicSymbol(row, 2)
	if symbol["rank"] != 2 || symbol["entity_name"] != "run" {
		t.Fatalf("symbol = %v, want rank and name", symbol)
	}
	terms := CodeTopicSearchTerms("restart workers", "find callers", []string{"custom"})
	if len(terms) == 0 {
		t.Fatal("search terms are empty")
	}
	found := false
	for _, term := range terms {
		if term == "custom" {
			found = true
		}
	}
	if !found {
		t.Fatalf("search terms = %v, want the explicit term kept", terms)
	}
	if got := CrossRepoDeadCodeConfidenceLabel(0.95); got != "high" {
		t.Fatalf("0.95 label = %q, want high", got)
	}
	if got := CrossRepoDeadCodeConfidenceLabel(0); got != "unknown" {
		t.Fatalf("0 label = %q, want unknown", got)
	}
	incoming := map[string]code.DeadCodeIncomingEdge{}
	MergeStrongestDeadCodeIncomingEdge(incoming, "e1", code.DeadCodeIncomingEdge{MaxConfidence: 0.7})
	MergeStrongestDeadCodeIncomingEdge(incoming, "e1", code.DeadCodeIncomingEdge{MaxConfidence: 0.2})
	if incoming["e1"].MaxConfidence != 0.7 {
		t.Fatalf("merged edge = %+v, want strongest kept", incoming["e1"])
	}
	if got := ResultContentEntityType(map[string]any{"labels": []any{"Function"}}); got != "Function" {
		t.Fatalf("entity type = %q, want Function", got)
	}
	if got := ResultContentEntityType(map[string]any{}); got != "" {
		t.Fatalf("labelless type = %q, want empty", got)
	}
	if !DeadCodeIncomingEdgeIsWeak(0) || DeadCodeIncomingEdgeIsWeak(1) {
		t.Fatal("weak-edge gate mismatch: zero weak, certain strong")
	}
	resp := CodeTopicResponse(CodeTopicInvestigationRequest{Topic: "restarts"}, []CodeTopicEvidenceRow{row}, false, false)
	if resp["topic"] != "restarts" {
		t.Fatalf("topic response = %v, want echoed topic", resp)
	}
	grant, blocked := LanguageQueryGrantFor(context.Background(), "repository:a")
	if blocked {
		t.Fatalf("background grant blocked = %v with %+v", blocked, grant)
	}
}
