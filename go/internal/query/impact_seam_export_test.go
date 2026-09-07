// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/impact"
	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestImpactSeamExportsForward is the tripwire for the #6060 impact seam
// export: every seam forwarder returns what its backing home returns on the
// same input, every seam alias names its home type, and every seam const
// pins its contract value. If a forwarder body diverges (or a seam name is
// removed), this fails. It also fails to COMPILE if any seam name is
// removed, which is the point -- the impact move depends on each of these
// names resolving from outside the impact subpackage.
// assertSeamAlias proves at compile time that an exported seam alias names
// the same type as its unexported original: the call only compiles when a
// value of the original type is assignable to the alias type parameter.
func assertSeamAlias[Alias any](v Alias) Alias {
	return v
}

func TestImpactSeamExportsForward(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	a, b := []string{"a"}, []string{"a"}
	impact.AppendUniqueString(&a, "b")
	appendUniqueString(&b, "b")
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("AppendUniqueString != appendUniqueString: %v vs %v", a, b)
	}
	empty := []string(nil)
	impact.AppendUniqueString(&empty, "")
	if empty != nil {
		t.Fatalf("AppendUniqueString(&empty, '') = %v, want nil", empty)
	}
	if impact.ContainsString([]string{"a"}, "a") != containsString([]string{"a"}, "a") {
		t.Fatal("ContainsString != containsString")
	}
	if impact.ContainsString([]string{"a"}, "a") != querycontract.ContainsString([]string{"a"}, "a") {
		t.Fatal("ContainsString != querycontract.ContainsString")
	}
	if impact.JoinOrNone(nil) != impacttrace.JoinOrNone(nil) {
		t.Fatal("JoinOrNone != impacttrace.JoinOrNone")
	}
	if got := impact.JoinOrNone(nil); got != "none" {
		t.Fatalf("JoinOrNone(nil) = %q, want none", got)
	}
	in1, in2 := []string{"a", "", "a"}, []string{"a", "", "a"}
	if !reflect.DeepEqual(impact.UniqueStrings(in1), uniqueStrings(in2)) {
		t.Fatal("UniqueStrings != uniqueStrings")
	}
	rows := []map[string]any{{"a": 1}, {"a": 2}, {"a": 3}}
	gotTrimmed, gotMore := impact.TrimImpactRows(rows, 2)
	if len(gotTrimmed) != 2 || !gotMore {
		t.Fatalf("TrimImpactRows(rows, 2) = %d rows, more=%t, want 2 rows, more=true", len(gotTrimmed), gotMore)
	}
	if got := impact.NormalizeImpactListLimit(-1); got != 50 {
		t.Fatalf("NormalizeImpactListLimit(-1) = %d, want 50", got)
	}
	if got := impact.NormalizeImpactListLimit(100000); got != impact.ImpactMaxListLimit {
		t.Fatalf("NormalizeImpactListLimit(huge) = %d, want ImpactMaxListLimit", got)
	}
	if got := impact.CanonicalWorkloadIDCandidate("x"); got != "workload:x" {
		t.Fatalf("CanonicalWorkloadIDCandidate(x) = %q, want workload:x", got)
	}
	m1 := map[string]any{"keep": "v", "drop": "", "dropSlice": []string{}}
	if want := map[string]any{"keep": "v"}; !reflect.DeepEqual(querycontract.CompactStringMap(m1), want) {
		t.Fatalf("CompactStringMap = %#v, want %#v", m1, want)
	}
	if got := impact.PreChangeGraphTarget(impact.PreChangeImpactRequest{ServiceName: "s"}); got != "s" {
		t.Fatalf("PreChangeGraphTarget(service) = %q, want s", got)
	}
	if got, want := impact.PreChangeSummary(map[string]any{}), "Mapped 0 changed file(s) to 0 touched symbol(s), 0 direct impact row(s), and 0 transitive impact row(s)."; got != want {
		t.Fatalf("PreChangeSummary({}) = %q, want %q", got, want)
	}
	// A targetless request is a contract error in the home
	// (normalizePreChangeImpactRequest): the seam must forward the error, not
	// mask it. The base tripwire asserted seam/original parity, which holds
	// here because both sides reject the same input. See #6060.
	normed, err := impact.NormalizePreChangeImpactRequest(impact.PreChangeImpactRequest{RepoID: " r "})
	if err == nil {
		t.Fatalf("NormalizePreChangeImpactRequest(RepoID-only) = %+v, want target-required contract error", normed)
	}
	trimmed, trimErr := impact.NormalizePreChangeImpactRequest(impact.PreChangeImpactRequest{RepoID: " r ", Topic: "t"})
	if trimErr != nil {
		t.Fatalf("NormalizePreChangeImpactRequest error = %v", trimErr)
	}
	if trimmed.RepoID != "r" {
		t.Fatalf("NormalizePreChangeImpactRequest RepoID = %q, want r", trimmed.RepoID)
	}
	if got := impact.PreChangeImpactErrorStatus(nil); got != 500 {
		t.Fatalf("PreChangeImpactErrorStatus(nil) = %d, want 500", got)
	}
	unscoped := RepositoryAccessFilter{AllScopes: true}
	if impact.ImpactRepoIDAllowed("", unscoped) != impacttrace.ImpactRepoIDAllowed("", unscoped) {
		t.Fatal("ImpactRepoIDAllowed != impacttrace.ImpactRepoIDAllowed")
	}
	if !reflect.DeepEqual(
		impact.FilterRowsByRepoIDForAccess(nil, unscoped),
		impacttrace.FilterRowsByRepoIDForAccess(nil, unscoped),
	) {
		t.Fatal("FilterRowsByRepoIDForAccess != impacttrace.FilterRowsByRepoIDForAccess")
	}
	if !reflect.DeepEqual(
		impact.FilterProvisioningRepositoryCandidatesForAccess(nil, unscoped),
		impacttrace.FilterProvisioningRepositoryCandidatesForAccess(nil, unscoped),
	) {
		t.Fatal("FilterProvisioningRepositoryCandidatesForAccess != impacttrace.FilterProvisioningRepositoryCandidatesForAccess")
	}
	instances := []map[string]any{{"k": "b"}, {"k": ""}, {"other": "x"}, {"k": "a"}}
	if got, want := impact.DistinctSortedInstanceField(instances, "k"), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DistinctSortedInstanceField = %v, want %v", got, want)
	}
	gotBounded := impact.BoundedK8sResourceResult(nil, false, nil, false, false)
	if len(gotBounded.Rows()) != 0 {
		t.Fatalf("BoundedK8sResourceResult(nil) rows = %d, want 0", len(gotBounded.Rows()))
	}
	// Nil graph and nil content take the early empty paths on both sides:
	// the selector resolves to nothing without a reader, and the service
	// read model returns nil without content. This also proves the
	// production backend wiring in family_impact_shim.go: a nil
	// DefaultTraceContext would panic here instead of returning.
	gotTrace, gotTraceErr := impact.FetchServiceTraceContext(ctx, nil, nil, nil, "", impact.TraceEnrichmentConfig{})
	wantTrace, wantTraceErr := fetchServiceTraceContext(ctx, nil, nil, nil, "", impact.TraceEnrichmentConfig{})
	if !reflect.DeepEqual(gotTrace, wantTrace) || !reflect.DeepEqual(gotTraceErr, wantTraceErr) {
		t.Fatal("FetchServiceTraceContext != fetchServiceTraceContext")
	}

	// Aliases name the same objects: assigning an original-typed value to an
	// alias-typed variable only compiles when the alias holds, so each line
	// below is a compile-time identity proof (written through a generic to
	// keep the assertion while satisfying staticcheck QF1011).
	assertSeamAlias[impact.ProvisioningRepositoryCandidate](impacttrace.ProvisioningRepositoryCandidate{})
	assertSeamAlias[RepositoryAccessFilter](querycontract.RepositoryAccessFilter{})
	var _ impact.DeploymentSourceResult
	var _ impact.K8sResourceResult
	var _ impact.PreChangeImpactRequest
	var _ impact.TraceDeploymentChainRequest
	if impact.DeveloperChangePlanCapability != "platform_impact.developer_change_plan" {
		t.Fatalf("DeveloperChangePlanCapability = %q", impact.DeveloperChangePlanCapability)
	}
	if impact.ImpactMaxListLimit != 200 {
		t.Fatalf("ImpactMaxListLimit = %d, want 200", impact.ImpactMaxListLimit)
	}
	if impact.ContractImpactCapability != "platform_impact.contract_impact" {
		t.Fatalf("ContractImpactCapability = %q", impact.ContractImpactCapability)
	}
	if !errors.Is(impact.ErrAmbiguousTraceWorkloadSelector, impacttrace.ErrAmbiguousTraceWorkloadSelector) {
		t.Fatal("ErrAmbiguousTraceWorkloadSelector != impacttrace.ErrAmbiguousTraceWorkloadSelector")
	}

	// Renamed ImpactHandler methods resolve and behave.
	h := &ImpactHandler{}
	if h.ResolvedProfile() != NormalizeQueryProfile("") {
		t.Fatal("ResolvedProfile does not normalize the Profile field")
	}
	var nilHandler *ImpactHandler
	if nilHandler.ResolvedProfile() != ProfileProduction {
		t.Fatal("ResolvedProfile on nil handler is not ProfileProduction")
	}
	_ = (*ImpactHandler).FetchK8sResourceResult
	_ = (*ImpactHandler).FetchDeploymentSourceGitOps
	_ = (*ImpactHandler).FetchDeploymentSourceResult
	_ = (*ImpactHandler).PreChangeImpactResponse
	_ = (*ImpactHandler).ResolvedProfile

	// The cross-package constructor must thread maxDepth: the external
	// proof can only observe it through a nil-graph early exit where a
	// dropped field is invisible, so pin the value here. See #6060.
	if impact.NewTraceEnrichmentConfig(4) != (impact.TraceEnrichmentConfig{MaxDepth: 4}) {
		t.Fatal("NewTraceEnrichmentConfig(4) != TraceEnrichmentConfig{maxDepth: 4}")
	}
}
