// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deployment

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func deploymentScopedAccess() querycontract.RepositoryAccessFilter {
	return querycontract.RepositoryAccessFilterFromContext(scopedDeploymentContext())
}

func TestImpactRepoIDAllowed(t *testing.T) {
	t.Parallel()

	unscoped := querycontract.RepositoryAccessFilter{AllScopes: true}
	if !ImpactRepoIDAllowed("", unscoped) {
		t.Fatal("unscoped caller with an empty id must be allowed")
	}
	scoped := deploymentScopedAccess()
	if !ImpactRepoIDAllowed("repo-a", scoped) {
		t.Fatal("granted repo must be allowed for the scoped caller")
	}
	if ImpactRepoIDAllowed("repo-b", scoped) {
		t.Fatal("ungranted repo must be denied for the scoped caller")
	}
	if ImpactRepoIDAllowed("", scoped) {
		t.Fatal("empty repo id must be denied for the scoped caller (deny-by-default)")
	}
}

func TestFilterRowsByRepoIDForAccess(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"repo_id": "repo-a", "name": "in-grant"},
		{"repo_id": "repo-b", "name": "out-of-grant"},
		{"name": "no-repo-tie"},
	}
	unscoped := querycontract.RepositoryAccessFilter{AllScopes: true}
	if got := FilterRowsByRepoIDForAccess(rows, unscoped); len(got) != 3 {
		t.Fatalf("unscoped rows = %v, want all three unchanged", got)
	}
	got := FilterRowsByRepoIDForAccess(rows, deploymentScopedAccess())
	if len(got) != 1 || got[0]["name"] != "in-grant" {
		t.Fatalf("scoped rows = %v, want only the granted repo (unbindable rows dropped)", got)
	}
}

func TestFilterProvisioningRepositoryCandidatesForAccess(t *testing.T) {
	t.Parallel()

	candidates := []ProvisioningRepositoryCandidate{
		{RepoID: "repo-a", RepoName: "granted"},
		{RepoID: "repo-b", RepoName: "denied"},
	}
	unscoped := querycontract.RepositoryAccessFilter{AllScopes: true}
	if got := FilterProvisioningRepositoryCandidatesForAccess(candidates, unscoped); len(got) != 2 {
		t.Fatalf("unscoped candidates = %v, want both", got)
	}
	got := FilterProvisioningRepositoryCandidatesForAccess(candidates, deploymentScopedAccess())
	if len(got) != 1 || got[0].RepoID != "repo-a" {
		t.Fatalf("scoped candidates = %+v, want only the granted repo", got)
	}
}

func TestDeploymentTraceRuntimeArtifactFamily(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"cloudformation_serverless": "cloudformation",
		"docker_compose":            "docker_compose",
		"dockerfile":                "docker",
		"terraform":                 "terraform",
		"  helm  ":                  "helm",
	}
	for artifact, want := range cases {
		if got := deploymentTraceRuntimeArtifactFamily(artifact); got != want {
			t.Errorf("deploymentTraceRuntimeArtifactFamily(%q) = %q, want %q", artifact, got, want)
		}
	}
}

func TestLiveIdentityAnchorFilter(t *testing.T) {
	t.Parallel()

	declared := liveIdentityAnchor{
		Kind: liveIdentityAnchorDeclaredObject, GroupVersionResource: "apps/v1/deployments",
		Namespace: "prod", Name: "api",
	}
	got := LiveIdentityAnchorFilter(declared, []string{"img:1"}, querycontract.RepositoryAccessFilter{AllScopes: true})
	if !got.AllScopes || got.GroupVersionResource != "apps/v1/deployments" || got.Namespace != "prod" || got.Name != "api" {
		t.Fatalf("declared filter = %+v, want identity carried with all-scopes", got)
	}
	tracked := liveIdentityAnchor{Kind: "argocd", TrackingID: "track-1"}
	scoped := LiveIdentityAnchorFilter(tracked, nil, deploymentScopedAccess())
	if scoped.AllScopes || scoped.TrackingID != "track-1" {
		t.Fatalf("scoped filter = %+v, want the tracking id with scoping preserved", scoped)
	}
	if len(scoped.AllowedRepositoryIDs) != 1 || scoped.AllowedRepositoryIDs[0] != "repo-a" {
		t.Fatalf("scoped filter grants = %+v, want the caller grant carried over", scoped)
	}
}

func TestResolvedImpactAnchorComparators(t *testing.T) {
	t.Parallel()

	anchor := ResolvedImpactAnchor{ID: "n1", Label: "Service", Labels: []string{"Service", "Workload"}}
	if got := anchor.Pattern("start", "id"); got != "(start:Service {id: $id})" {
		t.Fatalf("Pattern() = %q", got)
	}
	if !anchor.SamePhysicalNode(ResolvedImpactAnchor{ID: "n1", Label: "Service"}) {
		t.Fatal("same id and label must bind one node")
	}
	if !anchor.SamePhysicalNode(ResolvedImpactAnchor{ID: "n1", Label: "Deployment", Labels: []string{"Workload"}}) {
		t.Fatal("same id with a shared label must bind one node")
	}
	if anchor.SamePhysicalNode(ResolvedImpactAnchor{ID: "n2", Label: "Service"}) {
		t.Fatal("different ids must not bind one node")
	}
	if anchor.SamePhysicalNode(ResolvedImpactAnchor{ID: "n1", Label: "Deployment", Labels: []string{"Other"}}) {
		t.Fatal("same id without a shared label must not bind one node")
	}
}
