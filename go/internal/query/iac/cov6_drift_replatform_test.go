// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iac

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestCov6AWSRuntimeDriftOutcomes(t *testing.T) {
	t.Parallel()
	statuses := map[string]string{
		ManagementStatusManagedByTerraform:  awsRuntimeDriftOutcomeExact,
		ManagementStatusTerraformStateOnly:  awsRuntimeDriftOutcomeDerived,
		ManagementStatusTerraformConfigOnly: awsRuntimeDriftOutcomeDerived,
		ManagementStatusCloudOnly:           awsRuntimeDriftOutcomeDerived,
		ManagementStatusManagedByOtherIaC:   awsRuntimeDriftOutcomeDerived,
		ManagementStatusAmbiguous:           awsRuntimeDriftOutcomeAmbiguous,
		ManagementStatusStaleIaCCandidate:   awsRuntimeDriftOutcomeStale,
		ManagementStatusUnknown:             awsRuntimeDriftOutcomeUnknown,
		"something-new":                     awsRuntimeDriftOutcomeUnknown,
	}
	for status, want := range statuses {
		if got := awsRuntimeDriftOutcome(ManagementFindingRow{ManagementStatus: status}); got != want {
			t.Fatalf("drift outcome(%q) = %q, want %q", status, got, want)
		}
	}
	gated := ManagementFindingRow{SafetyGate: ManagementSafetyGate{ReviewRequired: true}}
	if got := awsRuntimeDriftPromotionOutcome(gated); got != awsRuntimeDriftPromotionRejected {
		t.Fatalf("gated promotion = %q, want rejected", got)
	}
	if got := awsRuntimeDriftPromotionOutcome(ManagementFindingRow{}); got != awsRuntimeDriftPromotionNotPromoted {
		t.Fatalf("open promotion = %q, want not_promoted", got)
	}
	if got := awsRuntimeDriftPromotionReason(gated); got != "safety_gate_requires_review" {
		t.Fatalf("gated reason = %q, want safety_gate_requires_review", got)
	}
	if got := awsRuntimeDriftPromotionReason(ManagementFindingRow{}); got != "read_model_only_no_ownership_promotion" {
		t.Fatalf("open reason = %q, want read-model note", got)
	}
	rows := awsRuntimeDriftFindingRows([]ManagementFindingRow{
		{ManagementStatus: ManagementStatusManagedByTerraform, ARN: "arn:b"},
		{ManagementStatus: ManagementStatusCloudOnly, ARN: "arn:a"},
		{ManagementStatus: ManagementStatusManagedByTerraform, ARN: "arn:a"},
	})
	if len(rows) != 3 || rows[0].Outcome != awsRuntimeDriftOutcomeExact {
		t.Fatalf("drift rows = %+v, want outcomes attached", rows)
	}
	groups := awsRuntimeDriftOutcomeGroups(rows)
	if len(groups) != 2 || groups[0].Outcome > groups[1].Outcome {
		t.Fatalf("outcome groups = %+v, want 2 sorted groups", groups)
	}
	if groups[0].Count+groups[1].Count != 3 {
		t.Fatalf("grouped counts = %+v, want 3 total", groups)
	}
	story := awsRuntimeDriftFindingsStory(ManagementFilter{AccountID: "123"}, rows, 9)
	if !strings.Contains(story, "9") || !strings.Contains(story, "123") {
		t.Fatalf("drift story = %q, want totals and scope", story)
	}
	bare := awsRuntimeDriftFindingsStory(ManagementFilter{}, nil, 0)
	if !strings.Contains(bare, "requested AWS scope") {
		t.Fatalf("scopeless story = %q, want default scope", bare)
	}
}

func TestCov6DeadIaCPagingHelpers(t *testing.T) {
	t.Parallel()
	req := deadIaCRequest{}
	normalizeDeadIaCPaging(&req)
	if req.Limit != deadDefaultLimit || req.Offset != 0 {
		t.Fatalf("default paging = %+v, want defaults", req)
	}
	over := deadIaCRequest{Limit: 1 << 30, Offset: -5}
	normalizeDeadIaCPaging(&over)
	if over.Limit != deadMaxLimit || over.Offset != 0 {
		t.Fatalf("clamped paging = %+v, want max/0", over)
	}
	findings := []deadIaCFinding{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	if got := pageDeadIaCFindings(findings, 2, 0); len(got) != 2 {
		t.Fatalf("first page = %d, want 2", len(got))
	}
	if got := pageDeadIaCFindings(findings, 2, 99); got != nil {
		t.Fatalf("past-end page = %v, want nil", got)
	}
	if deadIaCTruncated(0, 2, 2) || !deadIaCTruncated(0, 2, 3) {
		t.Fatal("truncation gate mismatch")
	}
	if got := deadIaCNextOffset(0, 2, 2); got != nil {
		t.Fatalf("complete next offset = %v, want nil", got)
	}
	if got := deadIaCNextOffset(0, 2, 3); got == nil || *got != 2 {
		t.Fatalf("partial next offset = %v, want 2", got)
	}
	if got := normalizeDeadIaCFamilies([]string{" Terraform ", "", "terraform", "CLOUDFORMATION"}); len(got) != 2 || got[0] != "cloudformation" {
		t.Fatalf("families = %v, want sorted deduped lowercase", got)
	}
	// A nil handler enriches nothing and panics never.
	var nilHandler *Handler
	nilHandler.enrichDeadIaCRepoNames(context.Background(), []deadIaCFinding{{ID: "a"}})
	materialized := materializedDeadIaCFindings([]ReachabilityFindingRow{
		{ID: "a", Family: "terraform", Evidence: []string{"e1"}, Limitations: []string{"l1"}},
	})
	if len(materialized) != 1 || materialized[0].Family != "terraform" || len(materialized[0].Evidence) != 1 {
		t.Fatalf("materialized = %+v, want projected finding", materialized)
	}
}

func TestCov6TerraformImportScopeHelpers(t *testing.T) {
	t.Parallel()
	finding := ManagementFindingRow{AccountID: " 123 ", Region: "us-east-1"}
	filter := ManagementFilter{}
	if got := terraformImportAccountID(finding, filter); got != "123" {
		t.Fatalf("finding account = %q, want 123", got)
	}
	if got := terraformImportRegion(finding, filter); got != "us-east-1" {
		t.Fatalf("finding region = %q, want us-east-1", got)
	}
	filterOnly := ManagementFilter{AccountID: "999", Region: "eu-west-1"}
	if got := terraformImportAccountID(ManagementFindingRow{}, filterOnly); got != "999" {
		t.Fatalf("filter account = %q, want 999", got)
	}
	scoped := ManagementFilter{ScopeID: "aws:111:us-west-2:ec2"}
	if got := terraformImportAccountID(ManagementFindingRow{}, scoped); got != "111" {
		t.Fatalf("scope account = %q, want 111", got)
	}
	if got := terraformImportRegion(ManagementFindingRow{}, scoped); got != "us-west-2" {
		t.Fatalf("scope region = %q, want us-west-2", got)
	}
	if parts := terraformImportScopeParts("bogus"); parts.accountID != "" || parts.region != "" {
		t.Fatalf("bogus scope = %+v, want empty", parts)
	}
	if got := terraformResourceName("My Bucket!"); got != "my_bucket" {
		t.Fatalf("resource name = %q, want my_bucket", got)
	}
	if got := terraformResourceName("!!!"); got != "resource" {
		t.Fatalf("blank resource name = %q, want resource", got)
	}
	if got := terraformResourceName("9lives"); got != "resource_9lives" {
		t.Fatalf("numeric resource name = %q, want resource_ prefix", got)
	}
	if got := terraformImportBlock("aws_s3_bucket.x", "arn:1"); !strings.Contains(got, "aws_s3_bucket.x") {
		t.Fatalf("import block = %q, want address", got)
	}
	shaped := ManagementFindingRow{MatchedTerraformModulePath: "mod/vpc"}
	if got := terraformImportDestinationHint(shaped); got != "module:mod/vpc" {
		t.Fatalf("module hint = %q", got)
	}
	filed := ManagementFindingRow{MatchedTerraformConfigFile: "main.tf"}
	if got := terraformImportDestinationHint(filed); got != "file:main.tf" {
		t.Fatalf("file hint = %q", got)
	}
	if got := terraformImportDestinationHint(ManagementFindingRow{}); got != "operator_selected_target" {
		t.Fatalf("bare hint = %q", got)
	}
	if got := terraformImportConfigurationShape(shaped); got != "module_shaped" {
		t.Fatalf("module shape = %q", got)
	}
	if got := terraformImportConfigurationShape(ManagementFindingRow{}); got != "flat_starter" {
		t.Fatalf("flat shape = %q", got)
	}
	if got := terraformProviderAlias("123", "us-east-1"); got == "" {
		t.Fatal("provider alias must not be empty for account+region")
	}
	if got := terraformProviderAlias("", ""); got != "" {
		t.Fatalf("blank alias = %q, want empty", got)
	}
	if !terraformImportStringSliceContains([]string{"a"}, "a") || terraformImportStringSliceContains([]string{"a"}, "b") {
		t.Fatal("string slice contains mismatch")
	}
}

func TestCov6ReplatformingConfidenceAndOwners(t *testing.T) {
	t.Parallel()
	for confidence, want := range map[float64]string{0: "unspecified", -1: "unspecified", 0.95: "high", 0.7: "medium", 0.2: "low"} {
		if got := replatformingConfidenceLabel(confidence); got != want {
			t.Fatalf("confidence(%v) = %q, want %q", confidence, got, want)
		}
	}
	if got := replatformingStableID(ManagementFindingRow{ARN: "arn:1"}); got != "arn:1" {
		t.Fatalf("stable id = %q, want ARN", got)
	}
	if got := replatformingStableID(ManagementFindingRow{ID: "f1"}); got != "f1" {
		t.Fatalf("stable id fallback = %q, want finding id", got)
	}
	if got := replatformingStableID(ManagementFindingRow{}); got != "unknown" {
		t.Fatalf("bare stable id = %q, want unknown", got)
	}
	if got := replatformingDeclaredDetail(ManagementFindingRow{MatchedTerraformModulePath: "m"}); !strings.Contains(got, "m") {
		t.Fatalf("module detail = %q", got)
	}
	if got := replatformingDeclaredDetail(ManagementFindingRow{MatchedTerraformConfigFile: "f.tf"}); !strings.Contains(got, "f.tf") {
		t.Fatalf("file detail = %q", got)
	}
	if got := replatformingDeclaredDetail(ManagementFindingRow{MatchedOtherIaCSource: "cfn"}); !strings.Contains(got, "cfn") {
		t.Fatalf("other detail = %q", got)
	}
	solo := replatformingOwnerGroup("service", []string{" web ", "web"}, ReplatformingSourceStateExact)
	if len(solo) != 1 || solo[0].Confidence != "single_candidate" {
		t.Fatalf("solo owner = %+v, want single candidate", solo)
	}
	if got := replatformingOwnerGroup("service", []string{"", " "}, ReplatformingSourceStateExact); got != nil {
		t.Fatalf("blank owners = %v, want nil", got)
	}
	duel := replatformingOwnerGroup("service", []string{"a", "b"}, ReplatformingSourceStateExact)
	if len(duel) != 2 || duel[0].Confidence != "ambiguous" || len(duel[0].AmbiguityReasons) == 0 {
		t.Fatalf("competing owners = %+v, want ambiguity reasons", duel)
	}
	gated := replatformingOwnerGroup("service", []string{"a"}, ReplatformingSourceStateAmbiguous)
	if len(gated) != 1 || len(gated[0].AmbiguityReasons) == 0 {
		t.Fatalf("ambiguous-state owner = %+v, want reasons", gated)
	}
	if got := replatformingAmbiguityReasons("service", []string{"a", "b"}, ReplatformingSourceStateExact); len(got) != 1 {
		t.Fatalf("multi reasons = %v, want 1", got)
	}
	if got := replatformingDedupeNonEmpty([]string{" b ", "", "a", "b"}); len(got) != 2 {
		t.Fatalf("deduped = %v, want 2", got)
	}
	if got := replatformingSingleOwnerKey(nil); got != ReplatformingRollupUnattributedKey {
		t.Fatalf("empty owner key = %q, want unattributed", got)
	}
	if got := replatformingSingleOwnerKey([]string{"solo"}); got != "solo" {
		t.Fatalf("solo key = %q", got)
	}
	if got := replatformingSingleOwnerKey([]string{"a", "b"}); got != ReplatformingRollupAmbiguousKey {
		t.Fatalf("duel key = %q, want ambiguous", got)
	}
	if got := replatformingAccountKey(ManagementFindingRow{}, ManagementFilter{}); got != ReplatformingRollupUnattributedKey {
		t.Fatalf("bare account key = %q, want unattributed", got)
	}
	if got := replatformingAccountKey(ManagementFindingRow{AccountID: "1"}, ManagementFilter{}); got != "1" {
		t.Fatalf("finding account key = %q", got)
	}
}

func TestCov6ReplatformingSourceStates(t *testing.T) {
	t.Parallel()
	states := AllReplatformingSourceStates()
	if len(states) == 0 {
		t.Fatal("source state taxonomy is empty")
	}
	states[0] = "mutated"
	if AllReplatformingSourceStates()[0] == "mutated" {
		t.Fatal("taxonomy must be a copy")
	}
	if !ReplatformingSourceStateExact.Valid() || ReplatformingSourceState("bogus").Valid() {
		t.Fatal("taxonomy membership mismatch")
	}
	for status, want := range map[string]ReplatformingSourceState{
		ManagementStatusManagedByTerraform: ReplatformingSourceStateExact,
		ManagementStatusCloudOnly:          ReplatformingSourceStateDerived,
		ManagementStatusAmbiguous:          ReplatformingSourceStateAmbiguous,
		ManagementStatusStaleIaCCandidate:  ReplatformingSourceStateStale,
		ManagementStatusUnknown:            ReplatformingSourceStateUnknown,
		"future-status":                    ReplatformingSourceStateUnknown,
	} {
		if got := ReplatformingSourceStateForManagementStatus(status); got != want {
			t.Fatalf("management status(%q) = %q, want %q", status, got, want)
		}
	}
	if got := ReplatformingSourceStateForMultiCloudQueryState("bogus"); got != ReplatformingSourceStateUnknown {
		t.Fatalf("multicloud bogus = %q, want unknown", got)
	}
	if got := ReplatformingSourceStateForMultiCloudQueryState("exact"); got != ReplatformingSourceStateExact {
		t.Fatalf("multicloud exact = %q", got)
	}
	if got := ResolveReplatformingSourceState(ManagementStatusManagedByTerraform, true); got != ReplatformingSourceStateRejected {
		t.Fatalf("gated resolve = %q, want rejected", got)
	}
	if got := ResolveReplatformingSourceState(ManagementStatusManagedByTerraform, false); got != ReplatformingSourceStateExact {
		t.Fatalf("open resolve = %q, want exact", got)
	}
	if got := ReplatformingSourceStateExact.TruthLevel(); got != querycontract.TruthLevelExact {
		t.Fatalf("exact truth level = %q, want exact", got)
	}
	if got := ReplatformingSourceStateUnknown.TruthLevel(); got != ReplatformingSourceStateAmbiguous.TruthLevel() {
		t.Fatal("uncertain states must share the fallback level")
	}
	var nilPlan *ReplatformingPlan
	if got := nilPlan.RollupTruthLevel(); got != querycontract.TruthLevelDerived {
		t.Fatalf("empty plan rollup = %q, want derived", got)
	}
	if got := normalizedOwnerKind(" Service "); got != "service" {
		t.Fatalf("owner kind = %q, want service", got)
	}
}

func TestCov6ReplatformingSelectors(t *testing.T) {
	t.Parallel()
	empty := replatformingSelectorScopedEmptyResponse(25)
	readiness, ok := empty["readiness"].(map[string]any)
	if !ok || readiness["state"] != "no_authorized_scopes" {
		t.Fatalf("scoped-empty readiness = %v, want no_authorized_scopes", empty["readiness"])
	}
	if got := ReplatformingSelectorLabel(ReplatformingSelectorScope{Service: "ec2", Region: "us-east-1"}); !strings.Contains(got, "account unknown") {
		t.Fatalf("accountless label = %q", got)
	}
	if got := ReplatformingSelectorLabel(ReplatformingSelectorScope{Service: "ec2", Region: "us-east-1", AccountID: "123456789"}); !strings.Contains(got, "6789") {
		t.Fatalf("account label = %q, want last-4 suffix", got)
	}
	page := replatformingSelectorResponse(ReplatformingSelectorPage{Scopes: []ReplatformingSelectorScope{
		{ScopeID: "s1", FindingCount: 0},
		{ScopeID: "s2", FindingCount: 3},
	}}, 25)
	if page["count"] != 2 || page["empty_scope_count"] != 1 {
		t.Fatalf("selector page = %v, want 2 scopes with 1 empty", page)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/selectors", nil)
	if limit, ok := replatformingSelectorLimit(w, r); !ok || limit != replatformingSelectorDefaultLimit {
		t.Fatalf("default selector limit = (%d,%v)", limit, ok)
	}
	bad := httptest.NewRequest("GET", "/selectors?limit=bogus", nil)
	if _, ok := replatformingSelectorLimit(httptest.NewRecorder(), bad); ok {
		t.Fatal("bogus selector limit must be rejected")
	}
	ready := replatformingSelectorReadiness([]map[string]any{{"scope_id": "s1"}})
	if ready["state"] != "ready" {
		t.Fatalf("populated readiness = %v, want ready", ready)
	}
	if got := replatformingSelectorReadiness(nil)["state"]; got != "collector_evidence_absent" {
		t.Fatalf("absent readiness = %v", got)
	}
}

func TestCov6ManagementStatusDerivation(t *testing.T) {
	t.Parallel()
	full := managementStatusInput{}
	full.recordEvidence("aws_cloud_resource")
	full.recordEvidence("terraform_state_resource")
	full.recordEvidence("terraform_config_resource")
	if got := deriveIaCManagementStatus(full); got != ManagementStatusManagedByTerraform {
		t.Fatalf("full evidence = %q, want managed", got)
	}
	conflicted := managementStatusInput{}
	conflicted.recordEvidence("terraform_state_resource")
	conflicted.recordEvidence("other_iac_resource")
	if got := deriveIaCManagementStatus(conflicted); got != ManagementStatusAmbiguous {
		t.Fatalf("conflicting evidence = %q, want ambiguous", got)
	}
	gapped := managementStatusInput{}
	gapped.recordEvidence("permission_gap")
	if got := deriveIaCManagementStatus(gapped); got != ManagementStatusUnknown {
		t.Fatalf("coverage gap = %q, want unknown", got)
	}
	stale := managementStatusInput{}
	stale.recordEvidence("stale_snapshot")
	if got := deriveIaCManagementStatus(stale); got != ManagementStatusStaleIaCCandidate {
		t.Fatalf("stale evidence = %q, want stale candidate", got)
	}
	onlyCloud := managementStatusInput{HasCloudEvidence: true}
	if got := deriveIaCManagementStatus(onlyCloud); got != ManagementStatusCloudOnly {
		t.Fatalf("cloud only = %q", got)
	}
	if got := normalizeIaCManagementStatus("bogus", ManagementStatusCloudOnly); got != ManagementStatusCloudOnly {
		t.Fatalf("fallback normalize = %q", got)
	}
	if got := normalizeIaCManagementStatus("bogus", "bogus"); got != ManagementStatusUnknown {
		t.Fatalf("double-bogus normalize = %q", got)
	}
	if got := missingEvidenceForManagementStatus(ManagementStatusCloudOnly); len(got) != 2 {
		t.Fatalf("cloud-only missing = %v, want 2", got)
	}
	if got := missingEvidenceForManagementStatus(ManagementStatusManagedByTerraform); got != nil {
		t.Fatalf("managed missing = %v, want nil", got)
	}
	if got := recommendedActionForManagementStatus(ManagementStatusAmbiguous); got == "" {
		t.Fatal("ambiguous action must be named")
	}
	if got := recommendedActionForManagementStatus("bogus"); got != "review_evidence" {
		t.Fatalf("bogus action = %q, want review_evidence", got)
	}
	if got := firstNonEmpty("", "b"); got != "b" {
		t.Fatalf("firstNonEmpty = %q", got)
	}
	if got := firstNonEmptySlice(nil, []string{"x"}); len(got) != 1 {
		t.Fatalf("firstNonEmptySlice = %v", got)
	}
	var zero resourceListMetrics
	zero.recordDuration(context.Background(), "resource", 0.1)
	zero.recordError(context.Background(), "resource", "validation")
}

func TestCov6PacketItemValidation(t *testing.T) {
	t.Parallel()
	valid := MigrationPacketItem{
		ItemID: "i1", Provider: "aws", ResourceType: "aws_s3_bucket", StableID: "s1",
		SourceState: ReplatformingSourceStateExact,
		OwnerCandidates: []ReplatformingOwnerCandidate{
			{Kind: "service", Value: "web", Confidence: "single_candidate"},
		},
	}
	if err := valid.validate(); err != nil {
		t.Fatalf("valid item rejected: %v", err)
	}
	bare := MigrationPacketItem{}
	if err := bare.validate(); err == nil {
		t.Fatal("bare item validates, want rejection")
	}
	badState := valid
	badState.SourceState = "bogus"
	if err := badState.validate(); err == nil {
		t.Fatal("bogus source state validates, want rejection")
	}
	readyNoBlock := valid
	readyNoBlock.ImportCandidate = &ReplatformingImportCandidate{Status: ReplatformingImportStatusReady}
	if err := readyNoBlock.validate(); err == nil {
		t.Fatal("ready candidate without block validates, want rejection")
	}
	refusedNoReasons := valid
	refusedNoReasons.ImportCandidate = &ReplatformingImportCandidate{Status: ReplatformingImportStatusRefused}
	if err := refusedNoReasons.validate(); err == nil {
		t.Fatal("refused candidate without reasons validates, want rejection")
	}
	refusedWithBlock := valid
	refusedWithBlock.ImportCandidate = &ReplatformingImportCandidate{Status: ReplatformingImportStatusRefused, RefusalReasons: []string{"r"}, ImportBlock: "block"}
	if err := refusedWithBlock.validate(); err == nil {
		t.Fatal("refused candidate with block validates, want rejection")
	}
	bogusStatus := valid
	bogusStatus.ImportCandidate = &ReplatformingImportCandidate{Status: "bogus"}
	if err := bogusStatus.validate(); err == nil {
		t.Fatal("bogus candidate status validates, want rejection")
	}
	duel := valid
	duel.OwnerCandidates = []ReplatformingOwnerCandidate{
		{Kind: "service", Value: "a"},
		{Kind: "service", Value: "b"},
	}
	if err := duel.validate(); err == nil {
		t.Fatal("reason-free competitors validate, want rejection")
	}
	ambiguous := valid
	ambiguous.SourceState = ReplatformingSourceStateAmbiguous
	if err := ambiguous.validate(); err == nil {
		t.Fatal("reason-free owner on ambiguous item validates, want rejection")
	}
	dup := ReplatformingPlan{Items: []MigrationPacketItem{valid, valid}}
	if err := dup.Validate(); err == nil {
		t.Fatal("duplicate item ids validate, want rejection")
	}
}
