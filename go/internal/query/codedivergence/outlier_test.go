// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"strings"
	"testing"
)

// outlierGuardEdges builds the positive fixture: five handlers h-1..h-5, the
// first four calling the guard, all declared with confidence 0.9.
func outlierGuardEdges() map[string][]OutlierCallerEdge {
	edges := map[string][]OutlierCallerEdge{}
	for _, member := range []string{"h-1", "h-2", "h-3", "h-4"} {
		edges[member] = []OutlierCallerEdge{{
			CalleeID: "guard", CalleeName: "requireAuth",
			EdgeMethod: "declared", EdgeConfidence: 0.9,
		}}
	}
	return edges
}

func outlierGuardCohort() OutlierCohort {
	return OutlierCohort{
		Source: CohortRouter, Key: "widgets", Label: "/widgets routes",
		Members:      []string{"h-1", "h-2", "h-3", "h-4", "h-5"},
		TotalMembers: 5,
	}
}

// TestSelectOutliersPositive proves the issue's positive case: five
// handlers, four calling the guard, produce one verdict naming the guard at
// share 0.8 with h-5 the outlier and the weakest majority edge as
// confidence.
func TestSelectOutliersPositive(t *testing.T) {
	t.Parallel()

	verdicts := SelectOutliers(outlierGuardCohort(), outlierGuardEdges(), nil, DefaultOutlierParams())
	if len(verdicts) != 1 {
		t.Fatalf("verdicts = %d, want 1", len(verdicts))
	}
	verdict := verdicts[0]
	if verdict.CalleeID != "guard" || verdict.CalleeName != "requireAuth" {
		t.Errorf("callee = %q/%q, want guard/requireAuth", verdict.CalleeID, verdict.CalleeName)
	}
	if verdict.Share != 0.8 {
		t.Errorf("share = %v, want 0.8", verdict.Share)
	}
	if len(verdict.OutlierIDs) != 1 || verdict.OutlierIDs[0] != "h-5" {
		t.Errorf("outliers = %v, want [h-5]", verdict.OutlierIDs)
	}
	if len(verdict.CallerIDs) != 4 {
		t.Errorf("callers = %v, want 4", verdict.CallerIDs)
	}
	if verdict.Confidence != 0.9 {
		t.Errorf("confidence = %v, want 0.9", verdict.Confidence)
	}
	if verdict.Inferred {
		t.Error("inferred = true, want false on declared edges")
	}
}

// TestSelectOutliersBelowMinimumSize proves the negative case: a cohort of
// two never produces a verdict, however unanimous.
func TestSelectOutliersBelowMinimumSize(t *testing.T) {
	t.Parallel()

	cohort := OutlierCohort{
		Source: CohortPackage, Key: "pkg", Label: "pkg",
		Members: []string{"a-1", "a-2"}, TotalMembers: 2,
	}
	edges := map[string][]OutlierCallerEdge{
		"a-1": {{CalleeID: "guard", CalleeName: "requireAuth", EdgeMethod: "declared", EdgeConfidence: 0.9}},
		"a-2": {{CalleeID: "guard", CalleeName: "requireAuth", EdgeMethod: "declared", EdgeConfidence: 0.9}},
	}
	if verdicts := SelectOutliers(cohort, edges, nil, DefaultOutlierParams()); len(verdicts) != 0 {
		t.Errorf("verdicts = %d, want 0 below minimum cohort size", len(verdicts))
	}
}

// TestSelectOutliersBelowShare proves the negative case: a helper called by
// two of five (0.4) stays below the 0.6 floor and produces nothing.
func TestSelectOutliersBelowShare(t *testing.T) {
	t.Parallel()

	edges := map[string][]OutlierCallerEdge{
		"h-1": {{CalleeID: "helper", CalleeName: "format", EdgeMethod: "declared", EdgeConfidence: 0.9}},
		"h-2": {{CalleeID: "helper", CalleeName: "format", EdgeMethod: "declared", EdgeConfidence: 0.9}},
	}
	if verdicts := SelectOutliers(outlierGuardCohort(), edges, nil, DefaultOutlierParams()); len(verdicts) != 0 {
		t.Errorf("verdicts = %d, want 0 below share floor", len(verdicts))
	}
}

// TestSelectOutliersUnanimousCalisNoFinding proves a callee every member
// calls is a convention kept, not a finding: no outliers, no verdict.
func TestSelectOutliersUnanimousCalisNoFinding(t *testing.T) {
	t.Parallel()

	edges := outlierGuardEdges()
	edges["h-5"] = []OutlierCallerEdge{{
		CalleeID: "guard", CalleeName: "requireAuth",
		EdgeMethod: "declared", EdgeConfidence: 0.9,
	}}
	if verdicts := SelectOutliers(outlierGuardCohort(), edges, nil, DefaultOutlierParams()); len(verdicts) != 0 {
		t.Errorf("verdicts = %d, want 0 for unanimous callee", len(verdicts))
	}
}

// TestSelectOutliersWeakestConfidenceAndInferred proves confidence inherits
// the weakest majority edge and an inferred-edge majority is labelled.
func TestSelectOutliersWeakestConfidenceAndInferred(t *testing.T) {
	t.Parallel()

	edges := outlierGuardEdges()
	edges["h-1"] = []OutlierCallerEdge{{
		CalleeID: "guard", CalleeName: "requireAuth",
		EdgeMethod: "scope_unique_name", EdgeConfidence: 0.7,
	}}
	verdicts := SelectOutliers(outlierGuardCohort(), edges, nil, DefaultOutlierParams())
	if len(verdicts) != 1 {
		t.Fatalf("verdicts = %d, want 1", len(verdicts))
	}
	if verdicts[0].Confidence != 0.7 {
		t.Errorf("confidence = %v, want weakest 0.7", verdicts[0].Confidence)
	}
	if !verdicts[0].Inferred {
		t.Error("inferred = false, want true on scope_unique_name majority edge")
	}
}

// TestSelectOutliersUnspecifiedMethodIsNotInferred proves legacy edges
// without a recorded method stay unclassified, never inferred.
func TestSelectOutliersUnspecifiedMethodIsNotInferred(t *testing.T) {
	t.Parallel()

	edges := outlierGuardEdges()
	edges["h-1"] = []OutlierCallerEdge{{
		CalleeID: "guard", CalleeName: "requireAuth",
		EdgeMethod: "unspecified", EdgeConfidence: 0.95,
	}}
	verdicts := SelectOutliers(outlierGuardCohort(), edges, nil, DefaultOutlierParams())
	if len(verdicts) != 1 {
		t.Fatalf("verdicts = %d, want 1", len(verdicts))
	}
	if verdicts[0].Inferred {
		t.Error("inferred = true, want false on unspecified edges")
	}
}

// TestSelectOutliersMediationAttaches proves the ambiguous case: the outlier
// reaches the guard through a wrapper, so the verdict carries the mediation
// instead of a clean miss.
func TestSelectOutliersMediationAttaches(t *testing.T) {
	t.Parallel()

	edges := outlierGuardEdges()
	edges["h-5"] = []OutlierCallerEdge{{
		CalleeID: "wrap", CalleeName: "checkedHandler",
		EdgeMethod: "declared", EdgeConfidence: 0.9,
	}}
	mediated := map[string]map[string]OutlierMediation{
		"guard": {"h-5": {OutlierID: "h-5", MediatorID: "wrap", MediatorName: "checkedHandler"}},
	}
	verdicts := SelectOutliers(outlierGuardCohort(), edges, mediated, DefaultOutlierParams())
	if len(verdicts) != 1 {
		t.Fatalf("verdicts = %d, want 1", len(verdicts))
	}
	if len(verdicts[0].Mediated) != 1 || verdicts[0].Mediated[0].MediatorID != "wrap" {
		t.Errorf("mediated = %+v, want the h-5/wrap mediation", verdicts[0].Mediated)
	}
	if len(verdicts[0].OutlierIDs) != 1 {
		t.Errorf("outliers = %v, want [h-5] still listed", verdicts[0].OutlierIDs)
	}
}

func outlierGuardMembers() []Member {
	members := make([]Member, 0, 5)
	for i, id := range []string{"h-1", "h-2", "h-3", "h-4", "h-5"} {
		members = append(members, Member{
			EntityID: id, EntityName: "handle" + strings.TrimPrefix(id, "h-"),
			EntityType: "Function", RelativePath: "api/widgets.go",
			Language: "go", StartLine: 10 + i*20, EndLine: 25 + i*20, TokenCount: 120,
		})
	}
	return members
}

// TestAssembleOutlierFindingPositive proves the assembled finding: outliers
// lead, the score decomposes without remainder, and the detail names the
// cohort, callee, share, and outliers.
func TestAssembleOutlierFindingPositive(t *testing.T) {
	t.Parallel()

	verdicts := SelectOutliers(outlierGuardCohort(), outlierGuardEdges(), nil, DefaultOutlierParams())
	finding, ok := AssembleOutlierFinding("repo-a", outlierGuardCohort(), outlierGuardMembers(), outlierGuardEdges(), verdicts[0], false, DefaultOutlierParams())
	if !ok {
		t.Fatalf("AssembleOutlierFinding ok = false, suppressions=%v", finding.Suppressions)
	}
	if finding.Kind != KindConventionOutlier {
		t.Errorf("kind = %q, want %q", finding.Kind, KindConventionOutlier)
	}
	if len(finding.Members) != 5 || finding.Members[0].EntityID != "h-5" {
		t.Errorf("members lead with the outlier, got %v", finding.Members)
	}
	if finding.Score != 5*120 {
		t.Errorf("score = %d, want 600", finding.Score)
	}
	sum := 0
	for _, reason := range finding.Reasons {
		sum += reason.Value
	}
	if sum != finding.Score {
		t.Errorf("reasons sum = %d, score = %d", sum, finding.Score)
	}
	if finding.Outlier == nil {
		t.Fatal("detail is nil")
	}
	detail := finding.Outlier
	if detail.Source != CohortRouter || detail.Key != "widgets" {
		t.Errorf("cohort = %q/%q, want router/widgets", detail.Source, detail.Key)
	}
	if detail.CalleeID != "guard" || detail.Share != 0.8 {
		t.Errorf("callee/share = %q/%v, want guard/0.8", detail.CalleeID, detail.Share)
	}
	if len(detail.OutlierIDs) != 1 || detail.OutlierIDs[0] != "h-5" {
		t.Errorf("outliers = %v, want [h-5]", detail.OutlierIDs)
	}
	if detail.Truncated || detail.Inferred {
		t.Errorf("truncated/inferred = %v/%v, want false/false", detail.Truncated, detail.Inferred)
	}
	if finding.Confidence != 0.9 {
		t.Errorf("confidence = %v, want 0.9", finding.Confidence)
	}
}

// TestAssembleOutlierFindingScoreInvariant mutates each reason and asserts
// the sum tracks the score, per the package contract.
func TestAssembleOutlierFindingScoreInvariant(t *testing.T) {
	t.Parallel()

	verdicts := SelectOutliers(outlierGuardCohort(), outlierGuardEdges(), nil, DefaultOutlierParams())
	finding, ok := AssembleOutlierFinding("repo-a", outlierGuardCohort(), outlierGuardMembers(), outlierGuardEdges(), verdicts[0], false, DefaultOutlierParams())
	if !ok {
		t.Fatal("assembly failed")
	}
	for i := range finding.Reasons {
		mutated := finding
		mutated.Reasons = append([]Reason(nil), finding.Reasons...)
		mutated.Reasons[i].Value += 7
		sum := 0
		for _, reason := range mutated.Reasons {
			sum += reason.Value
		}
		if sum == finding.Score {
			t.Errorf("reason %d mutation did not move the sum off score %d", i, finding.Score)
		}
	}
}

// TestAssembleOutlierFindingSuppressesTestOutlier proves a test-file outlier
// suppresses out of the member set (counted) while the finding still holds
// on the survivors.
func TestAssembleOutlierFindingSuppressesTestOutlier(t *testing.T) {
	t.Parallel()

	members := outlierGuardMembers()
	members[4].RelativePath = "api/widgets_test.go"
	verdicts := SelectOutliers(outlierGuardCohort(), outlierGuardEdges(), nil, DefaultOutlierParams())
	finding, ok := AssembleOutlierFinding("repo-a", outlierGuardCohort(), members, outlierGuardEdges(), verdicts[0], false, DefaultOutlierParams())
	if ok {
		t.Fatal("assembly ok with the only outlier suppressed, want unqualified")
	}
	if finding.Suppressions[RuleTestFile] != 1 {
		t.Errorf("test_file suppressions = %v, want 1", finding.Suppressions)
	}
	if finding.Suppressions[RuleOutlierUnqualified] == 0 {
		t.Errorf("missing outlier_unqualified count: %v", finding.Suppressions)
	}
}

// TestAssembleOutlierFindingMediatedReason proves the ambiguous assembly
// carries the wrapper-mediated signal with no score weight.
func TestAssembleOutlierFindingMediatedReason(t *testing.T) {
	t.Parallel()

	edges := outlierGuardEdges()
	edges["h-5"] = []OutlierCallerEdge{{
		CalleeID: "wrap", CalleeName: "checkedHandler",
		EdgeMethod: "declared", EdgeConfidence: 0.9,
	}}
	mediated := map[string]map[string]OutlierMediation{
		"guard": {"h-5": {OutlierID: "h-5", MediatorID: "wrap", MediatorName: "checkedHandler"}},
	}
	verdicts := SelectOutliers(outlierGuardCohort(), edges, mediated, DefaultOutlierParams())
	finding, ok := AssembleOutlierFinding("repo-a", outlierGuardCohort(), outlierGuardMembers(), edges, verdicts[0], false, DefaultOutlierParams())
	if !ok {
		t.Fatal("assembly failed")
	}
	found := false
	for _, reason := range finding.Reasons {
		if reason.Code == ReasonOutlierMediated {
			found = true
			if reason.Value != 0 {
				t.Errorf("mediated reason value = %d, want 0", reason.Value)
			}
			if !strings.Contains(reason.Sentence, "wrap") {
				t.Errorf("mediated sentence misses the mediator: %q", reason.Sentence)
			}
		}
	}
	if !found {
		t.Errorf("no wrapper-mediated reason in %+v", finding.Reasons)
	}
	if finding.Outlier == nil || len(finding.Outlier.Mediated) != 1 {
		t.Errorf("detail mediations = %+v, want 1", finding.Outlier)
	}
}

// TestAssembleOutlierFindingInferredReason proves an inferred-edge majority
// is labelled with the zero-weight signal.
func TestAssembleOutlierFindingInferredReason(t *testing.T) {
	t.Parallel()

	edges := outlierGuardEdges()
	edges["h-1"] = []OutlierCallerEdge{{
		CalleeID: "guard", CalleeName: "requireAuth",
		EdgeMethod: "repo_unique_name", EdgeConfidence: 0.5,
	}}
	verdicts := SelectOutliers(outlierGuardCohort(), edges, nil, DefaultOutlierParams())
	finding, ok := AssembleOutlierFinding("repo-a", outlierGuardCohort(), outlierGuardMembers(), edges, verdicts[0], false, DefaultOutlierParams())
	if !ok {
		t.Fatal("assembly failed")
	}
	if finding.Outlier == nil || !finding.Outlier.Inferred {
		t.Errorf("detail inferred = %+v, want true", finding.Outlier)
	}
	if finding.Confidence != 0.5 {
		t.Errorf("confidence = %v, want weakest 0.5", finding.Confidence)
	}
}
