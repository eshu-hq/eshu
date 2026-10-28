// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package answer

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/evidence"
)

// TestCov7ClassifyAnswerTruthTable proves every classification arm, including
// the exact-level content-index fallthrough to derived.
func TestCov7ClassifyAnswerTruthTable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		truth *querycontract.TruthEnvelope
		want  querycontract.AnswerTruthClass
	}{
		{"nil truth", nil, querycontract.AnswerTruthUnsupported},
		{"semantic exact", &querycontract.TruthEnvelope{Basis: querycontract.TruthBasisSemanticFacts, Level: querycontract.TruthLevelExact}, querycontract.AnswerTruthSemanticObservation},
		{"authoritative exact", &querycontract.TruthEnvelope{Basis: querycontract.TruthBasisAuthoritativeGraph, Level: querycontract.TruthLevelExact}, querycontract.AnswerTruthDeterministic},
		{"no read exact", &querycontract.TruthEnvelope{Basis: querycontract.TruthBasisNoBackendRead, Level: querycontract.TruthLevelExact}, querycontract.AnswerTruthFallback},
		{"content derived", &querycontract.TruthEnvelope{Basis: querycontract.TruthBasisContentIndex, Level: querycontract.TruthLevelDerived}, querycontract.AnswerTruthCodeHint},
		{"content exact falls through", &querycontract.TruthEnvelope{Basis: querycontract.TruthBasisContentIndex, Level: querycontract.TruthLevelExact}, querycontract.AnswerTruthDerived},
		{"fallback level", &querycontract.TruthEnvelope{Basis: querycontract.TruthBasisAuthoritativeGraph, Level: querycontract.TruthLevelFallback}, querycontract.AnswerTruthFallback},
		{"derived default", &querycontract.TruthEnvelope{Basis: querycontract.TruthBasisAuthoritativeGraph, Level: querycontract.TruthLevelDerived}, querycontract.AnswerTruthDerived},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := ClassifyAnswerTruth(tc.truth); got != tc.want {
				t.Fatalf("ClassifyAnswerTruth = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCov7NewAnswerPacketUnsupportedPaths proves the nil-envelope, error, and
// truthless-envelope paths all land unsupported with the summary dropped.
func TestCov7NewAnswerPacketUnsupportedPaths(t *testing.T) {
	t.Parallel()

	nilPacket := NewAnswerPacket(AnswerPacketInput{Summary: "confident"})
	if nilPacket.Supported || nilPacket.TruthClass != querycontract.AnswerTruthUnsupported || nilPacket.Summary != "" || nilPacket.Truth != nil {
		t.Fatalf("nil envelope packet = %#v, want unsupported with dropped summary", nilPacket)
	}
	foundInternal := false
	for _, reason := range nilPacket.UnsupportedReasons {
		if strings.Contains(reason, "internal_error") {
			foundInternal = true
		}
	}
	if !foundInternal {
		t.Fatalf("nil envelope reasons = %#v, want internal_error reason", nilPacket.UnsupportedReasons)
	}

	errPacket := NewAnswerPacket(AnswerPacketInput{
		Summary:  "confident",
		Envelope: &querycontract.ResponseEnvelope{Error: &querycontract.ErrorEnvelope{Code: "boom", Message: "kaput"}},
	})
	if errPacket.Supported || errPacket.Partial || errPacket.Summary != "" {
		t.Fatalf("error packet = %#v, want unsupported non-partial with dropped summary", errPacket)
	}
	if len(errPacket.UnsupportedReasons) != 1 || errPacket.UnsupportedReasons[0] != "boom: kaput" {
		t.Fatalf("error reasons = %#v, want [boom: kaput]", errPacket.UnsupportedReasons)
	}

	if reason := unsupportedAnswerPacketReason(nil); reason != "answer unsupported" {
		t.Fatalf("nil error reason = %q, want answer unsupported", reason)
	}
	if reason := unsupportedAnswerPacketReason(&querycontract.ErrorEnvelope{Message: "only"}); reason != "only" {
		t.Fatalf("codeless reason = %q, want message", reason)
	}
	if reason := unsupportedAnswerPacketReason(&querycontract.ErrorEnvelope{Code: "gone"}); reason != "gone" {
		t.Fatalf("messageless reason = %q, want code", reason)
	}
}

// TestCov7NewAnswerPacketSupportedFresh proves a fresh exact answer stays
// supported, keeps its summary, classifies, clones truth, and embeds data.
func TestCov7NewAnswerPacketSupportedFresh(t *testing.T) {
	t.Parallel()

	truth := &querycontract.TruthEnvelope{
		Basis:     querycontract.TruthBasisAuthoritativeGraph,
		Level:     querycontract.TruthLevelExact,
		Freshness: querycontract.TruthFreshness{State: querycontract.FreshnessFresh},
	}
	envelope := &querycontract.ResponseEnvelope{Data: map[string]any{"rows": 1}, Truth: truth}
	packet := NewAnswerPacket(AnswerPacketInput{
		PromptFamily: "code.topic",
		Question:     "  what? ",
		PrimaryTool:  "tool",
		PrimaryRoute: "/route",
		Summary:      "  all good ",
		ResultRef:    "eshu://x",
		EmbedResult:  true,
		Envelope:     envelope,
		EvidenceHandles: []evidence.EvidenceCitationHandle{
			{Kind: "file", RepoID: "r1", RelativePath: "a.go"},
		},
	})
	if !packet.Supported || packet.Partial {
		t.Fatalf("packet = %#v, want supported non-partial", packet)
	}
	if packet.TruthClass != querycontract.AnswerTruthDeterministic {
		t.Fatalf("TruthClass = %q, want deterministic", packet.TruthClass)
	}
	if packet.Summary != "all good" || packet.Question != "what?" || packet.PromptFamily != "code.topic" {
		t.Fatalf("packet = %#v, want trimmed fields with kept summary", packet)
	}
	if packet.Truth == truth {
		t.Fatalf("Truth shares the input pointer, want a clone")
	}
	if packet.Truth.Basis != truth.Basis {
		t.Fatalf("cloned Truth = %#v, want matching basis", packet.Truth)
	}
	result, ok := packet.Result.(map[string]any)
	if !ok || result["rows"] != 1 {
		t.Fatalf("Result = %#v, want embedded envelope data", packet.Result)
	}
}

// TestCov7NewAnswerPacketPartialPaths proves truncation, missing evidence,
// stale/building freshness, and the no-evidence summary drop each mark the
// packet partial with reasons.
func TestCov7NewAnswerPacketPartialPaths(t *testing.T) {
	t.Parallel()

	fresh := func() *querycontract.TruthEnvelope {
		return &querycontract.TruthEnvelope{
			Basis:     querycontract.TruthBasisAuthoritativeGraph,
			Level:     querycontract.TruthLevelExact,
			Freshness: querycontract.TruthFreshness{State: querycontract.FreshnessFresh},
		}
	}

	truncated := NewAnswerPacket(AnswerPacketInput{
		Summary:   "kept",
		Truncated: true,
		Envelope:  &querycontract.ResponseEnvelope{Truth: fresh()},
		EvidenceHandles: []evidence.EvidenceCitationHandle{
			{Kind: "file", RepoID: "r1", RelativePath: "a.go"},
		},
	})
	if !truncated.Partial || truncated.Summary != "kept" {
		t.Fatalf("truncated packet = %#v, want partial with kept summary", truncated)
	}

	missing := NewAnswerPacket(AnswerPacketInput{
		Summary:  "kept",
		Envelope: &querycontract.ResponseEnvelope{Truth: fresh()},
		MissingEvidence: []evidence.EvidenceCitationHandle{
			{Kind: "file"},
		},
		EvidenceHandles: []evidence.EvidenceCitationHandle{
			{Kind: "file", RepoID: "r1", RelativePath: "a.go"},
		},
	})
	if !missing.Partial {
		t.Fatalf("missing-evidence packet = %#v, want partial", missing)
	}

	stale := fresh()
	stale.Freshness = querycontract.TruthFreshness{
		State:     querycontract.FreshnessStale,
		Cause:     querycontract.FreshnessCausePendingRepoGeneration,
		NextCheck: &querycontract.FreshnessNextCheck{Tool: "get_index_status"},
	}
	stalePacket := NewAnswerPacket(AnswerPacketInput{
		Summary:  "kept",
		Envelope: &querycontract.ResponseEnvelope{Truth: stale},
		EvidenceHandles: []evidence.EvidenceCitationHandle{
			{Kind: "file", RepoID: "r1", RelativePath: "a.go"},
		},
	})
	if !stalePacket.Partial {
		t.Fatalf("stale packet = %#v, want partial", stalePacket)
	}
	foundCause, foundCall := false, false
	for _, reason := range stalePacket.UnsupportedReasons {
		if strings.Contains(reason, "pending_repo_generation") {
			foundCause = true
		}
	}
	for _, call := range stalePacket.RecommendedNextCalls {
		if call["tool"] == "get_index_status" {
			foundCall = true
		}
	}
	if !foundCause || !foundCall {
		t.Fatalf("stale packet reasons = %#v calls = %#v, want cause and drilldown",
			stalePacket.UnsupportedReasons, stalePacket.RecommendedNextCalls)
	}

	// A pre-existing identical drilldown is not appended twice.
	seeded := NewAnswerPacket(AnswerPacketInput{
		Summary:              "kept",
		Envelope:             &querycontract.ResponseEnvelope{Truth: stale},
		RecommendedNextCalls: []map[string]any{FreshnessNextCheckAsRecommendedCall(*stale.Freshness.NextCheck)},
		EvidenceHandles: []evidence.EvidenceCitationHandle{
			{Kind: "file", RepoID: "r1", RelativePath: "a.go"},
		},
	})
	if len(seeded.RecommendedNextCalls) != 1 {
		t.Fatalf("seeded calls = %#v, want dedup to one", seeded.RecommendedNextCalls)
	}

	building := fresh()
	building.Freshness = querycontract.TruthFreshness{State: querycontract.FreshnessBuilding}
	buildingPacket := NewAnswerPacket(AnswerPacketInput{
		Summary:    "dropped",
		NoEvidence: true,
		Envelope:   &querycontract.ResponseEnvelope{Truth: building},
	})
	if !buildingPacket.Partial || buildingPacket.Summary != "" {
		t.Fatalf("no-evidence packet = %#v, want partial with dropped summary", buildingPacket)
	}

	unavailable := fresh()
	unavailable.Freshness = querycontract.TruthFreshness{State: querycontract.FreshnessUnavailable}
	unavailablePacket := NewAnswerPacket(AnswerPacketInput{
		Summary:  "kept",
		Envelope: &querycontract.ResponseEnvelope{Truth: unavailable},
		EvidenceHandles: []evidence.EvidenceCitationHandle{
			{Kind: "file", RepoID: "r1", RelativePath: "a.go"},
		},
	})
	if unavailablePacket.Partial {
		t.Fatalf("unavailable-freshness packet = %#v, want non-partial", unavailablePacket)
	}
}

// TestCov7FreshnessReasonAndNextCall proves cause-gated reason rendering and
// the bounded next-call map shape.
func TestCov7FreshnessReasonAndNextCall(t *testing.T) {
	t.Parallel()

	if got := FreshnessReason("base", "bogus"); got != "base" {
		t.Fatalf("invalid cause reason = %q, want base", got)
	}
	if got := FreshnessReason("base", querycontract.FreshnessCauseReducerBacklog); got != "base (cause: reducer_backlog)" {
		t.Fatalf("valid cause reason = %q, want cause suffix", got)
	}
	if call := FreshnessNextCheckAsRecommendedCall(querycontract.FreshnessNextCheck{}); len(call) != 0 {
		t.Fatalf("empty next check = %#v, want empty map", call)
	}
	call := FreshnessNextCheckAsRecommendedCall(querycontract.FreshnessNextCheck{
		Tool: "t", Route: "r", Reason: "why", Params: map[string]string{"k": "v"},
	})
	if call["tool"] != "t" || call["route"] != "r" || call["reason"] != "why" {
		t.Fatalf("next call = %#v, want tool/route/reason", call)
	}
	params, ok := call["params"].(map[string]any)
	if !ok || params["k"] != "v" {
		t.Fatalf("next call params = %#v, want copied params", call["params"])
	}
	if !answerPacketRecommendedCallsEqual(map[string]any{"tool": "t"}, map[string]any{"tool": "t", "extra": 1}) {
		t.Fatalf("recommended calls compare = false, want true on shared keys")
	}
	if answerPacketRecommendedCallsEqual(map[string]any{"tool": "a"}, map[string]any{"tool": "b"}) {
		t.Fatalf("recommended calls compare = true, want false on tool mismatch")
	}
	if cloned := CloneTruthEnvelope(nil); cloned != nil {
		t.Fatalf("CloneTruthEnvelope(nil) = %#v, want nil", cloned)
	}
}

// TestCov7AnswerPacketCompanion proves the companion attaches without
// mutating the input and the code-topic helpers shape their outputs.
func TestCov7AnswerPacketCompanion(t *testing.T) {
	t.Parallel()

	if got := WithAnswerPacketCompanion(nil, nil, AnswerPacketCompanionInput{}); got != nil {
		t.Fatalf("nil companion = %#v, want nil", got)
	}
	truth := &querycontract.TruthEnvelope{
		Basis:     querycontract.TruthBasisAuthoritativeGraph,
		Level:     querycontract.TruthLevelExact,
		Freshness: querycontract.TruthFreshness{State: querycontract.FreshnessFresh},
	}
	data := map[string]any{"story": "svc does things"}
	out := WithAnswerPacketCompanion(data, truth, AnswerPacketCompanionInput{PromptFamily: "service.story"})
	packet, ok := out["answer_packet"].(AnswerPacket)
	if !ok || !packet.Supported {
		t.Fatalf("companion packet = %#v, want supported packet", out["answer_packet"])
	}
	if _, mutated := data["answer_packet"]; mutated {
		t.Fatalf("input data mutated by companion")
	}

	if got := CodeTopicAnswerSummary(map[string]any{"count": 0}); got != "" {
		t.Fatalf("zero summary = %q, want empty", got)
	}
	if got := CodeTopicAnswerSummary(map[string]any{"count": 3}); got != "Found 3 ranked code-topic evidence group(s)." {
		t.Fatalf("summary = %q, want count sentence", got)
	}
	if limitations := CodeTopicAnswerLimitations(map[string]any{}); len(limitations) != 0 {
		t.Fatalf("quiet limitations = %#v, want none", limitations)
	}
	limitations := CodeTopicAnswerLimitations(map[string]any{"truncated": true, "deadline_truncated": true})
	if len(limitations) != 2 {
		t.Fatalf("cut limitations = %#v, want truncation + deadline rows", limitations)
	}
	handles := CodeTopicEvidenceHandles(map[string]any{"evidence_groups": []any{
		map[string]any{"source_handle": map[string]any{"repo_id": "r1", "relative_path": "a.go", "start_line": 2, "end_line": 9}},
		map[string]any{"source_handle": map[string]any{}},
		map[string]any{"other": 1},
	}})
	if len(handles) != 1 || handles[0].Kind != "source" || handles[0].StartLine != 2 || handles[0].EndLine != 9 {
		t.Fatalf("handles = %#v, want one shaped source handle", handles)
	}
	story := ServiceStoryAnswerData("svc", map[string]any{"story": "svc does things"}, truth)
	storyPacket, ok := story["answer_packet"].(AnswerPacket)
	if !ok || storyPacket.Question != "Tell the story for service svc." || storyPacket.Summary != "svc does things" {
		t.Fatalf("story companion = %#v, want service question and summary", story["answer_packet"])
	}
}
