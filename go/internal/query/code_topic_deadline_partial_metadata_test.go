// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/answer"
)

// TestCodeTopicPageFittingDeadlinePartialMetadataAgreesWithPacket pins the
// #8543 contract: a code-topic page that fits its limit (truncated=false) but
// is deadline-truncated (the candidate pool hit its work budget, surfaced in
// coverage) must produce a metadata-level signal that agrees with the
// answer packet's partial=true. Today metadata is silent (truncated=false, no
// result_truncated limitation, empty partial_reasons) while the packet says
// partial.
func TestCodeTopicPageFittingDeadlinePartialMetadataAgreesWithPacket(t *testing.T) {
	t.Parallel()

	req := codequery.CodeTopicInvestigationRequest{
		Topic:  "repo sync authentication",
		RepoID: "repo-payments",
		Limit:  25,
		Terms:  []string{"repo", "sync", "authentication"},
	}
	rows := []codequery.CodeTopicEvidenceRow{{
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
	// The page fits: one row under a limit of 25, so data.truncated=false.
	data := codequery.CodeTopicResponse(req, rows, false)
	// The deadline/work-budget stop is surfaced in coverage, mirroring what a
	// deadline-truncated producer reports alongside the page.
	coverage, ok := data["coverage"].(map[string]any)
	if !ok {
		t.Fatal("code topic response has no coverage map")
	}
	coverage["deadline_truncated"] = true
	if data["truncated"] != false {
		t.Fatalf("data.truncated = %v, want false (page-fitting fixture)", data["truncated"])
	}

	// Attach the packet companion exactly as the route does in
	// codeTopicAnswerData: same prompt family, tool, summary, limitations,
	// truncation, evidence, and follow-up inputs.
	truth := BuildTruthEnvelope(ProfileProduction, codequery.CodeTopicCapability,
		TruthBasisContentIndex, "resolved from bounded content-index topic investigation")
	packetData := answer.WithAnswerPacketCompanion(data, truth, answer.AnswerPacketCompanionInput{
		PromptFamily:         "code.topic",
		Question:             req.Topic,
		PrimaryTool:          "investigate_code_topic",
		PrimaryRoute:         "/api/v0/code/topics/investigate",
		Summary:              answer.CodeTopicAnswerSummary(data),
		ResultRef:            "eshu://api-result/code/topics/investigate",
		Limitations:          answer.CodeTopicAnswerLimitations(data),
		Truncated:            querycontract.BoolVal(data, "truncated"),
		NoEvidence:           querycontract.IntVal(data, "count") == 0,
		EvidenceHandles:      answer.CodeTopicEvidenceHandles(data),
		RecommendedNextCalls: querycontract.MapSliceValue(data, "recommended_next_calls"),
	})
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
	if len(metadata.PartialReasons) == 0 {
		t.Fatal("answer_metadata.partial_reasons is empty while answer_packet.partial=true")
	}
}
