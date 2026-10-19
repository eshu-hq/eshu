// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/answer"
)

// TestCodeTopicPageFittingDeadlinePartialMetadataAgreesWithPacket pins the
// #8558 contract: a code-topic page that fits its limit (truncated=false)
// but was cut by the #7008 dispatch-budget deadline
// (data/coverage.deadline_truncated=true, the producer signal for a bounded
// deadline partial) must produce a metadata-level signal that agrees with
// the answer packet's partial=true. It exercises the real route wiring:
// CodeTopicResponse for the page plus codeTopicAnswerData for the packet
// companion. The pool-stop pins from #8549 stay unmodified alongside it.
func TestCodeTopicPageFittingDeadlinePartialMetadataAgreesWithPacket(t *testing.T) {
	t.Parallel()

	req := CodeTopicInvestigationRequest{
		Topic:  "repo sync authentication",
		RepoID: "repo-payments",
		Limit:  25,
		Terms:  []string{"repo", "sync", "authentication"},
	}
	rows := []CodeTopicEvidenceRow{{
		SourceKind:   "entity",
		RepoID:       "repo-payments",
		RelativePath: "go/internal/reposync/auth.go",
		EntityID:     "entity-auth",
		EntityName:   "resolveAuth",
		EntityType:   "Function",
		StartLine:    10,
		EndLine:      44,
	}}
	// The page fits: one row under a limit of 25, so data.truncated=false,
	// while the producer reports the deadline cut in data and coverage.
	// Fourth arg true marks a deadline partial; PoolTruncated stays false
	// so the deadline marker stays distinct from the pool-stop marker.
	data := CodeTopicResponse(req, rows, false, true)
	if BoolVal(data, "truncated") {
		t.Fatalf("data.truncated = %v, want false (page-fitting fixture)", data["truncated"])
	}
	if !BoolVal(data, "deadline_truncated") {
		t.Fatal("data.deadline_truncated = false, want true from the deadline partial")
	}
	if !querycontract.BoolVal(querycontract.MapValue(data, "coverage"), "deadline_truncated") {
		t.Fatal("coverage.deadline_truncated = false, want true from the deadline partial")
	}
	if querycontract.BoolVal(querycontract.MapValue(data, "coverage"), "candidate_pool_truncated") {
		t.Fatal("coverage.candidate_pool_truncated = true, want false (deadline marker stays distinct)")
	}

	truth := BuildTruthEnvelope(ProfileProduction, CodeTopicCapability,
		TruthBasisContentIndex, "resolved from bounded content-index topic investigation")
	packetData := codeTopicAnswerData(req, data, truth)
	packet, ok := packetData["answer_packet"].(answer.AnswerPacket)
	if !ok {
		t.Fatal("code topic response has no answer_packet companion")
	}
	if !packet.Partial {
		t.Fatal("answer_packet.partial = false, want true on a deadline-truncated page")
	}

	metadata, ok := answer.AnswerMetadataFromData(data)
	if !ok {
		t.Fatal("AnswerMetadataFromData ok = false, want true")
	}
	if !metadata.Truncated {
		t.Fatal("answer_metadata.truncated = false, want true on a deadline-truncated page")
	}
	for _, reason := range metadata.PartialReasons {
		if reason == "result_truncated" {
			return
		}
	}
	t.Fatalf("answer_metadata.partial_reasons = %q, want result_truncated while answer_packet.partial=true",
		metadata.PartialReasons)
}
