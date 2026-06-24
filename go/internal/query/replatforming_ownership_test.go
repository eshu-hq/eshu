// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"testing"
)

func ownershipFinding(arn, status, kind string, services, environments []string) IaCManagementFindingRow {
	finding := rollupFinding(arn, status, kind, services, environments)
	normalizeIaCManagementFindingSafety(&finding)
	return finding
}

// candidateValues collects the values of every candidate of one kind so a test
// can assert the candidate set without depending on slice order.
func candidateValues(packet replatformingOwnershipPacket, kind string) []string {
	var out []string
	for _, candidate := range packet.OwnerCandidates {
		if candidate.Kind == kind {
			out = append(out, candidate.Value)
		}
	}
	return out
}

func candidateForKind(packet replatformingOwnershipPacket, kind string) (ReplatformingOwnerCandidate, bool) {
	for _, candidate := range packet.OwnerCandidates {
		if candidate.Kind == kind {
			return candidate, true
		}
	}
	return ReplatformingOwnerCandidate{}, false
}

func TestBuildOwnershipPacketCloudOnlyNoServiceMatch(t *testing.T) {
	t.Parallel()

	finding := ownershipFinding(
		"arn:aws:s3:us-east-1:123456789012:bucket/orphaned-logs",
		managementStatusCloudOnly,
		findingKindOrphanedCloudResource,
		nil,
		nil,
	)
	filter := IaCManagementFilter{AccountID: "123456789012"}
	packet := buildReplatformingOwnershipPacket(finding, filter)

	if packet.SourceState != ReplatformingSourceStateDerived {
		t.Fatalf("source_state = %q, want derived", packet.SourceState)
	}
	if len(candidateValues(packet, ownershipCandidateKindService)) != 0 {
		t.Fatalf("cloud-only finding must not fabricate a service owner: %#v", packet.OwnerCandidates)
	}
	if !containsString(packet.MissingEvidence, "service_attribution") {
		t.Fatalf("missing_evidence must record absent service attribution: %#v", packet.MissingEvidence)
	}
	if !containsString(packet.MissingEvidence, "environment_attribution") {
		t.Fatalf("missing_evidence must record absent environment attribution: %#v", packet.MissingEvidence)
	}
	if len(packet.RecommendedNextCalls) == 0 {
		t.Fatal("a no-match finding must recommend a follow-up call")
	}
}

func TestBuildOwnershipPacketSingleServiceIsDerivedNotExact(t *testing.T) {
	t.Parallel()

	finding := ownershipFinding(
		"arn:aws:lambda:us-east-1:123456789012:function/checkout",
		managementStatusCloudOnly,
		findingKindUnmanagedCloudResource,
		[]string{"checkout"},
		[]string{"prod"},
	)
	packet := buildReplatformingOwnershipPacket(finding, IaCManagementFilter{AccountID: "123456789012"})

	service, ok := candidateForKind(packet, ownershipCandidateKindService)
	if !ok {
		t.Fatal("expected a service candidate")
	}
	if service.Value != "checkout" {
		t.Fatalf("service candidate value = %q, want checkout", service.Value)
	}
	if service.Confidence == ownershipConfidenceExact {
		t.Fatalf("a reducer candidate must never be promoted to exact confidence: %#v", service)
	}
	if len(service.AmbiguityReasons) != 0 {
		t.Fatalf("a single candidate must carry no ambiguity reasons: %#v", service.AmbiguityReasons)
	}
}

func TestBuildOwnershipPacketAmbiguousServiceCandidatesCarryReasons(t *testing.T) {
	t.Parallel()

	finding := ownershipFinding(
		"arn:aws:lambda:us-east-1:123456789012:function/shared",
		managementStatusCloudOnly,
		findingKindUnmanagedCloudResource,
		[]string{"billing", "checkout"},
		nil,
	)
	packet := buildReplatformingOwnershipPacket(finding, IaCManagementFilter{AccountID: "123456789012"})

	values := candidateValues(packet, ownershipCandidateKindService)
	if len(values) != 2 {
		t.Fatalf("expected two contested service candidates, got %#v", values)
	}
	for _, candidate := range packet.OwnerCandidates {
		if candidate.Kind != ownershipCandidateKindService {
			continue
		}
		if candidate.Confidence != ownershipConfidenceAmbiguous {
			t.Fatalf("contested candidate confidence = %q, want ambiguous: %#v", candidate.Confidence, candidate)
		}
		if len(candidate.AmbiguityReasons) == 0 {
			t.Fatalf("contested candidate must carry ambiguity reasons: %#v", candidate)
		}
	}
}

func TestBuildOwnershipPacketStateOnlyExposesModuleAndConfig(t *testing.T) {
	t.Parallel()

	finding := ownershipFinding(
		"arn:aws:dynamodb:us-east-1:123456789012:table/orders",
		managementStatusTerraformStateOnly,
		findingKindUnmanagedCloudResource,
		nil,
		nil,
	)
	finding.MatchedTerraformStateAddress = "aws_dynamodb_table.orders"
	finding.MatchedTerraformConfigFile = "infra/orders/main.tf"
	finding.MatchedTerraformModulePath = "module.orders"
	packet := buildReplatformingOwnershipPacket(finding, IaCManagementFilter{AccountID: "123456789012"})

	if _, ok := candidateForKind(packet, ownershipCandidateKindModule); !ok {
		t.Fatalf("state-only finding with a module path must surface a module candidate: %#v", packet.OwnerCandidates)
	}
	if packet.MatchedTerraformStateAddress != "aws_dynamodb_table.orders" {
		t.Fatalf("matched state address dropped: %#v", packet)
	}
}

func TestBuildOwnershipPacketTagsAreProvenanceNeverOwner(t *testing.T) {
	t.Parallel()

	finding := ownershipFinding(
		"arn:aws:lambda:us-east-1:123456789012:function/tagged",
		managementStatusCloudOnly,
		findingKindOrphanedCloudResource,
		nil,
		nil,
	)
	finding.Tags = map[string]string{"team": "payments", "Environment": "prod"}
	packet := buildReplatformingOwnershipPacket(finding, IaCManagementFilter{AccountID: "123456789012"})

	for _, candidate := range packet.OwnerCandidates {
		if candidate.Confidence == ownershipConfidenceExact {
			t.Fatalf("a raw-tag coincidence must never become an exact owner: %#v", candidate)
		}
	}
	// A tag must not silently become an environment owner candidate.
	if vals := candidateValues(packet, ownershipCandidateKindEnvironment); containsString(vals, "prod") {
		t.Fatalf("raw tag value leaked into environment candidate: %#v", vals)
	}
}

func TestBuildOwnershipPacketRejectedFindingNeverImportReady(t *testing.T) {
	t.Parallel()

	finding := ownershipFinding(
		"arn:aws:iam:us-east-1:123456789012:role/admin",
		managementStatusCloudOnly,
		findingKindOrphanedCloudResource,
		nil,
		nil,
	)
	// IAM is security-sensitive; the safety gate must require review.
	if !finding.SafetyGate.ReviewRequired {
		t.Fatalf("expected security review required for IAM finding: %#v", finding.SafetyGate)
	}
	packet := buildReplatformingOwnershipPacket(finding, IaCManagementFilter{AccountID: "123456789012"})
	if packet.SourceState != ReplatformingSourceStateRejected {
		t.Fatalf("safety-gated finding source_state = %q, want rejected", packet.SourceState)
	}
}

func TestBuildOwnershipPacketSummaryReportsCandidatesAndAccount(t *testing.T) {
	t.Parallel()

	finding := ownershipFinding(
		"arn:aws:lambda:us-east-1:123456789012:function/single",
		managementStatusCloudOnly,
		findingKindUnmanagedCloudResource,
		[]string{"checkout"},
		[]string{"prod"},
	)
	summary := buildReplatformingOwnershipSummary([]IaCManagementFindingRow{finding}, IaCManagementFilter{AccountID: "123456789012"})
	if len(summary.Packets) != 1 {
		t.Fatalf("expected one ownership packet, got %d", len(summary.Packets))
	}
	if summary.AmbiguousCount != 0 {
		t.Fatalf("single-candidate finding must not count as ambiguous: %#v", summary)
	}
	if summary.UnattributedCount != 0 {
		t.Fatalf("attributed finding must not count as unattributed: %#v", summary)
	}
}
