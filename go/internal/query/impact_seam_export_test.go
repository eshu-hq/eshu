// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// TestImpactSeamExportsForward is the tripwire for the #6060 impact seam
// export: every exported alias names the same object as its unexported
// original, and every exported forwarder returns what the original returns
// on the same input. If a forwarder body diverges (or an alias is deleted),
// this fails. It also fails to COMPILE if any seam name is removed, which is
// the point -- the impact move PR depends on each of these names resolving
// from outside the future impact subpackage.
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
	AppendUniqueString(&a, "b")
	appendUniqueString(&b, "b")
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("AppendUniqueString != appendUniqueString: %v vs %v", a, b)
	}
	empty := []string(nil)
	AppendUniqueString(&empty, "")
	if empty != nil {
		t.Fatalf("AppendUniqueString(&empty, '') = %v, want nil", empty)
	}
	if ContainsString([]string{"a"}, "a") != containsString([]string{"a"}, "a") {
		t.Fatal("ContainsString != containsString")
	}
	if JoinOrNone(nil) != joinOrNone(nil) {
		t.Fatal("JoinOrNone != joinOrNone")
	}
	if got := JoinOrNone(nil); got != "none" {
		t.Fatalf("JoinOrNone(nil) = %q, want none", got)
	}
	in1, in2 := []string{"a", "", "a"}, []string{"a", "", "a"}
	if !reflect.DeepEqual(UniqueStrings(in1), uniqueStrings(in2)) {
		t.Fatal("UniqueStrings != uniqueStrings")
	}
	rows := []map[string]any{{"a": 1}, {"a": 2}, {"a": 3}}
	gotTrimmed, gotMore := TrimImpactRows(rows, 2)
	wantTrimmed, wantMore := trimImpactRows(rows, 2)
	if !reflect.DeepEqual(gotTrimmed, wantTrimmed) || gotMore != wantMore {
		t.Fatal("TrimImpactRows != trimImpactRows")
	}
	if FirstPositiveFloat(0, -1, 2.5) != firstPositiveFloat(0, -1, 2.5) {
		t.Fatal("FirstPositiveFloat != firstPositiveFloat")
	}
	if NormalizeImpactListLimit(-1) != normalizeImpactListLimit(-1) {
		t.Fatal("NormalizeImpactListLimit != normalizeImpactListLimit")
	}
	if CanonicalWorkloadIDCandidate("x") != canonicalWorkloadIDCandidate("x") {
		t.Fatal("CanonicalWorkloadIDCandidate != canonicalWorkloadIDCandidate")
	}
	m1 := map[string]any{"keep": "v", "drop": "", "dropSlice": []string{}}
	m2 := map[string]any{"keep": "v", "drop": "", "dropSlice": []string{}}
	if !reflect.DeepEqual(CompactStringMap(m1), compactStringMap(m2)) {
		t.Fatal("CompactStringMap != compactStringMap")
	}
	if !reflect.DeepEqual(
		DeploymentEvidenceDeliveryPaths(map[string]any{}),
		deploymentEvidenceDeliveryPaths(map[string]any{}),
	) {
		t.Fatal("DeploymentEvidenceDeliveryPaths != deploymentEvidenceDeliveryPaths")
	}
	entry := map[string]any{"type": "image_ref", "target": "t"}
	if NormalizedDeliveryPathKey(entry) != normalizedDeliveryPathKey(entry) {
		t.Fatal("NormalizedDeliveryPathKey != normalizedDeliveryPathKey")
	}
	if BoundedTraceEnrichmentLimit(0) != boundedTraceEnrichmentLimit(0) {
		t.Fatal("BoundedTraceEnrichmentLimit != boundedTraceEnrichmentLimit")
	}
	if PreChangeGraphTarget(PreChangeImpactRequest{ServiceName: "s"}) !=
		preChangeGraphTarget(preChangeImpactRequest{ServiceName: "s"}) {
		t.Fatal("PreChangeGraphTarget != preChangeGraphTarget")
	}
	if PreChangeSummary(map[string]any{}) != preChangeSummary(map[string]any{}) {
		t.Fatal("PreChangeSummary != preChangeSummary")
	}
	normed, err1 := NormalizePreChangeImpactRequest(PreChangeImpactRequest{RepoID: " r "})
	want, err2 := normalizePreChangeImpactRequest(preChangeImpactRequest{RepoID: " r "})
	if !reflect.DeepEqual(normed, want) || !reflect.DeepEqual(err1, err2) {
		t.Fatal("NormalizePreChangeImpactRequest != normalizePreChangeImpactRequest")
	}
	if PreChangeImpactErrorStatus(nil) != preChangeImpactErrorStatus(nil) {
		t.Fatal("PreChangeImpactErrorStatus != preChangeImpactErrorStatus")
	}
	unscoped := RepositoryAccessFilter{AllScopes: true}
	if ImpactRepoIDAllowed("", unscoped) != impactRepoIDAllowed("", unscoped) {
		t.Fatal("ImpactRepoIDAllowed != impactRepoIDAllowed")
	}
	if !reflect.DeepEqual(
		FilterRowsByRepoIDForAccess(nil, unscoped),
		filterRowsByRepoIDForAccess(nil, unscoped),
	) {
		t.Fatal("FilterRowsByRepoIDForAccess != filterRowsByRepoIDForAccess")
	}
	if !reflect.DeepEqual(
		FilterProvisioningRepositoryCandidatesForAccess(nil, unscoped),
		filterProvisioningRepositoryCandidatesForAccess(nil, unscoped),
	) {
		t.Fatal("FilterProvisioningRepositoryCandidatesForAccess != filterProvisioningRepositoryCandidatesForAccess")
	}
	gotRows, gotTrunc, gotErr := QueryProvisioningRepositoryCandidates(ctx, nil, "", 0)
	wantRows, wantTrunc, wantErr := queryProvisioningRepositoryCandidates(ctx, nil, "", 0)
	if !reflect.DeepEqual(gotRows, wantRows) || gotTrunc != wantTrunc || !reflect.DeepEqual(gotErr, wantErr) {
		t.Fatal("QueryProvisioningRepositoryCandidates != queryProvisioningRepositoryCandidates")
	}
	gotChains, gotErr := LoadProvisioningSourceChainsFromCandidates(ctx, nil, nil)
	wantChains, wantErr := loadProvisioningSourceChainsFromCandidates(ctx, nil, nil)
	if !reflect.DeepEqual(gotChains, wantChains) || !reflect.DeepEqual(gotErr, wantErr) {
		t.Fatal("LoadProvisioningSourceChainsFromCandidates != loadProvisioningSourceChainsFromCandidates")
	}
	instances := []map[string]any{{"k": "b"}, {"k": ""}, {"other": "x"}, {"k": "a"}}
	if got, want := DistinctSortedInstanceField(instances, "k"), distinctSortedInstanceField(instances, "k"); !reflect.DeepEqual(got, want) || !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatal("DistinctSortedInstanceField != distinctSortedInstanceField")
	}
	gotBounded, wantBounded := BoundedK8sResourceResult(nil, false, nil, false, false), boundedK8sResourceResult(nil, false, nil, false, false)
	if !reflect.DeepEqual(gotBounded, wantBounded) {
		t.Fatal("BoundedK8sResourceResult != boundedK8sResourceResult")
	}
	// Nil graph and nil content take the early empty paths on both sides:
	// the selector resolves to nothing without a reader, and the service
	// read model returns nil without content.
	gotTrace, gotTraceErr := FetchServiceTraceContext(ctx, nil, nil, nil, "", TraceEnrichmentConfig{})
	wantTrace, wantTraceErr := fetchServiceTraceContext(ctx, nil, nil, nil, "", traceEnrichmentConfig{})
	if !reflect.DeepEqual(gotTrace, wantTrace) || !reflect.DeepEqual(gotTraceErr, wantTraceErr) {
		t.Fatal("FetchServiceTraceContext != fetchServiceTraceContext")
	}
	gotConsumers, gotTrunc, gotErr := LoadConsumerRepositoryEnrichmentFromCandidates(
		ctx, nil, nil, "", "", nil, 0, nil, false, false)
	wantConsumers, wantTrunc, wantErr := loadConsumerRepositoryEnrichmentFromCandidates(
		ctx, nil, nil, "", "", nil, 0, nil, false, false)
	if !reflect.DeepEqual(gotConsumers, wantConsumers) || gotTrunc != wantTrunc || !reflect.DeepEqual(gotErr, wantErr) {
		t.Fatal("LoadConsumerRepositoryEnrichmentFromCandidates != loadConsumerRepositoryEnrichmentFromCandidates")
	}

	// Aliases name the same objects: assigning an original-typed value to an
	// alias-typed variable only compiles when the alias holds, so each line
	// below is a compile-time identity proof (written through a generic to
	// keep the assertion while satisfying staticcheck QF1011).
	assertSeamAlias[DeploymentSourceResult](deploymentSourceResult{})
	assertSeamAlias[K8sResourceResult](k8sResourceResult{})
	assertSeamAlias[ProvisioningRepositoryCandidate](provisioningRepositoryCandidate{})
	assertSeamAlias[TraceEnrichmentConfig](traceEnrichmentConfig{})
	assertSeamAlias[PreChangeImpactRequest](preChangeImpactRequest{})
	assertSeamAlias[RepositoryAccessFilter](repositoryAccessFilter{})
	if DeveloperChangePlanCapability != developerChangePlanCapability {
		t.Fatal("DeveloperChangePlanCapability != developerChangePlanCapability")
	}
	if ImpactMaxListLimit != impactMaxListLimit {
		t.Fatal("ImpactMaxListLimit != impactMaxListLimit")
	}
	if !errors.Is(ErrAmbiguousTraceWorkloadSelector, errAmbiguousTraceWorkloadSelector) {
		t.Fatal("ErrAmbiguousTraceWorkloadSelector != errAmbiguousTraceWorkloadSelector")
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
}
