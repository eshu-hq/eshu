// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	workloadhandle "github.com/eshu-hq/eshu/go/internal/reducer/workload/handle"
)

// P5 row of the #5385 migration test table (reducer-side half): two
// repositories, alpha and beta, each defining a workload named "checkout" in
// a different environment. The projection seam must emit two split Workload
// nodes and two split instances on the repository-scoped key, keep each
// row's DEFINES ownership on its own repository, derive exactly one
// SAME_NAME pair between the scoped ids, mint no duplicate endpoint across
// the pair, and leave a legacy name-only handle ambiguous (it spans both
// repositories, so it resolves to neither).
//
// The live-graph half (both nodes and both instances readable, admitted-only
// sibling counts) rides the T1 live proof, which seeds the same pair shape.
func TestWorkloadIdentitySplitAcrossRepositories(t *testing.T) {
	t.Parallel()

	candidates := []WorkloadCandidate{
		{
			RepoID: "repository:r_alpha", RepoName: "alpha", WorkloadName: "checkout",
			Classification: "service", Confidence: 0.95,
			APIEndpoints: []APIEndpointSignal{{Path: "/healthz", Methods: []string{"GET"}}},
		},
		{
			RepoID: "repository:r_beta", RepoName: "beta", WorkloadName: "checkout",
			Classification: "service", Confidence: 0.95,
			APIEndpoints: []APIEndpointSignal{{Path: "/healthz", Methods: []string{"GET"}}},
		},
	}
	deploymentEnvironments := map[string][]string{
		"repository:r_alpha": {"prod"},
		"repository:r_beta":  {"staging"},
	}

	projection := BuildProjectionRows(candidates, deploymentEnvironments)

	if len(projection.WorkloadRows) != 2 {
		t.Fatalf("WorkloadRows = %d, want 2 split siblings", len(projection.WorkloadRows))
	}
	byID := map[string]WorkloadRow{}
	for _, row := range projection.WorkloadRows {
		byID[row.WorkloadID] = row
	}
	alpha, ok := byID["workload:repository:r_alpha:checkout"]
	if !ok {
		t.Fatalf("WorkloadRows = %v, want the alpha scoped node", projection.WorkloadRows)
	}
	beta, ok := byID["workload:repository:r_beta:checkout"]
	if !ok {
		t.Fatalf("WorkloadRows = %v, want the beta scoped node", projection.WorkloadRows)
	}
	if alpha.RepoID != "repository:r_alpha" || beta.RepoID != "repository:r_beta" {
		t.Fatalf("DEFINES ownership crossed: alpha=%q beta=%q", alpha.RepoID, beta.RepoID)
	}

	if len(projection.InstanceRows) != 2 {
		t.Fatalf("InstanceRows = %d, want 2 split instances", len(projection.InstanceRows))
	}
	instances := map[string]InstanceRow{}
	for _, row := range projection.InstanceRows {
		instances[row.InstanceID] = row
	}
	alphaInst, ok := instances["workload-instance:repository:r_alpha:checkout:prod"]
	if !ok || alphaInst.RepoID != "repository:r_alpha" || alphaInst.Environment != "prod" {
		t.Fatalf("InstanceRows = %v, want the alpha prod instance", projection.InstanceRows)
	}
	betaInst, ok := instances["workload-instance:repository:r_beta:checkout:staging"]
	if !ok || betaInst.RepoID != "repository:r_beta" || betaInst.Environment != "staging" {
		t.Fatalf("InstanceRows = %v, want the beta staging instance", projection.InstanceRows)
	}

	pairs := sameNamePairs(projection, []WorkloadSibling{
		{ID: beta.WorkloadID, Name: beta.WorkloadName, RepoID: beta.RepoID},
	})
	if len(pairs) != 1 {
		t.Fatalf("sameNamePairs = %v, want exactly the alpha/beta pair", pairs)
	}
	if pairs[0]["a"] != alpha.WorkloadID || pairs[0]["b"] != beta.WorkloadID {
		t.Fatalf("pair = %v, want a=%q b=%q", pairs[0], alpha.WorkloadID, beta.WorkloadID)
	}
	if pairs[0]["name"] != "checkout" {
		t.Fatalf("pair name = %v, want checkout", pairs[0]["name"])
	}

	seenEndpoints := map[string]string{}
	for _, row := range projection.EndpointRows {
		if owner, dup := seenEndpoints[row.EndpointID]; dup {
			t.Fatalf("duplicate endpoint %q shared by %q and %q", row.EndpointID, owner, row.WorkloadID)
		}
		seenEndpoints[row.EndpointID] = row.WorkloadID
	}

	resolution := workloadhandle.Resolve(
		"workload:checkout",
		[]workloadhandle.Candidate{
			{ID: alpha.WorkloadID, Name: alpha.WorkloadName, RepoID: alpha.RepoID},
			{ID: beta.WorkloadID, Name: beta.WorkloadName, RepoID: beta.RepoID},
		},
		nil,
	)
	if resolution.Outcome != workloadhandle.OutcomeAmbiguous {
		t.Fatalf("legacy handle outcome = %v, want ambiguous (spans two repositories)", resolution.Outcome)
	}
	if resolution.WorkloadID != "" {
		t.Fatalf("ambiguous handle resolved to %q, want no node", resolution.WorkloadID)
	}
}
