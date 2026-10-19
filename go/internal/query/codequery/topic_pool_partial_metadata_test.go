// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/answer"
)

// TestCodeTopicPageFittingPoolPartialMetadataAgreesWithPacket pins the #8543
// contract: a code-topic page that fits its limit (truncated=false) but hit
// the #7008 dispatch-budget pool cap (coverage.candidate_pool_truncated=true,
// the producer signal for a budget/deadline-truncated page) must produce a
// metadata-level signal that agrees with the answer packet's partial=true.
// It exercises the real route wiring: CodeTopicResponse for the page plus
// codeTopicAnswerData for the packet companion.
func TestCodeTopicPageFittingPoolPartialMetadataAgreesWithPacket(t *testing.T) {
	t.Parallel()

	req := CodeTopicInvestigationRequest{
		Topic:  "repo sync authentication",
		RepoID: "repo-payments",
		Limit:  25,
		Terms:  []string{"repo", "sync", "authentication"},
	}
	rows := []CodeTopicEvidenceRow{{
		SourceKind:    "entity",
		RepoID:        "repo-payments",
		RelativePath:  "go/internal/reposync/auth.go",
		EntityID:      "entity-auth",
		EntityName:    "resolveAuth",
		EntityType:    "Function",
		StartLine:     10,
		EndLine:       44,
		PoolTruncated: true,
	}}
	// The page fits: one row under a limit of 25, so data.truncated=false,
	// while the producer reports the pool-cap stop in coverage.
	data := CodeTopicResponse(req, rows, false)
	if BoolVal(data, "truncated") {
		t.Fatalf("data.truncated = %v, want false (page-fitting fixture)", data["truncated"])
	}
	if !querycontract.BoolVal(querycontract.MapValue(data, "coverage"), "candidate_pool_truncated") {
		t.Fatal("coverage.candidate_pool_truncated = false, want true from the PoolTruncated row")
	}

	truth := BuildTruthEnvelope(ProfileProduction, CodeTopicCapability,
		TruthBasisContentIndex, "resolved from bounded content-index topic investigation")
	packetData := codeTopicAnswerData(req, data, truth)
	packet, ok := packetData["answer_packet"].(answer.AnswerPacket)
	if !ok {
		t.Fatal("code topic response has no answer_packet companion")
	}
	if !packet.Partial {
		t.Fatal("answer_packet.partial = false, want true on a pool-truncated page")
	}

	metadata, ok := answer.AnswerMetadataFromData(data)
	if !ok {
		t.Fatal("AnswerMetadataFromData ok = false, want true")
	}
	if !metadata.Truncated {
		t.Fatal("answer_metadata.truncated = false, want true on a pool-truncated page")
	}
	for _, reason := range metadata.PartialReasons {
		if reason == "result_truncated" {
			return
		}
	}
	t.Fatalf("answer_metadata.partial_reasons = %q, want result_truncated while answer_packet.partial=true",
		metadata.PartialReasons)
}
