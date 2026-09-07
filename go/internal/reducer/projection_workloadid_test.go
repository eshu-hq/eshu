// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/workloadid"
)

// Every identifier BuildProjectionRows emits must equal the workloadid
// constructor recomputed from the row's own fields. This pins that rows
// track the single constructors (#5385): a future format divergence
// between a row and its constructor fails here, so the repository-scoped
// re-key lands inside those constructors rather than at each call site.
// It does not pin today's routing on its own — a faithful restore of the
// old inline format string yields byte-identical values on row-shaped
// inputs — so current routing proof is
// TestProjectionBlankEnvironmentDropsInstanceRow plus
// TestWorkloadIDsRouteThroughConstructors, which scans the package sources
// for hand-built workload and workload-instance `Sprintf` / `"workload:" +`
// construction sites.
func TestProjectionWorkloadIDsMatchConstructors(t *testing.T) {
	t.Parallel()
	candidates := []WorkloadCandidate{
		{
			RepoID:        "repo:alpha",
			RepoName:      "checkout",
			WorkloadName:  "  checkout  ",
			ResourceKinds: []string{"Deployment", "Service"},
			Confidence:    0.98,
			Provenance:    []string{"k8s_resource"},
		},
		{
			RepoID:        "repo:beta",
			RepoName:      "checkout",
			ResourceKinds: []string{"Deployment", "Service"},
			Confidence:    0.98,
			Provenance:    []string{"k8s_resource"},
		},
	}
	deploymentEnvs := map[string][]string{
		"repo:alpha": {"production"},
		"repo:beta":  {"staging"},
	}
	result := BuildProjectionRows(candidates, deploymentEnvs)
	if len(result.WorkloadRows) == 0 || len(result.InstanceRows) == 0 {
		t.Fatal("expected workload and instance rows, got none: vacuous pass guard")
	}
	for _, row := range result.WorkloadRows {
		want := workloadid.NewWorkloadID(row.RepoID, row.WorkloadName).String()
		if row.WorkloadID != want {
			t.Errorf("WorkloadRow.WorkloadID = %q, constructor recomputes %q", row.WorkloadID, want)
		}
	}
	for _, row := range result.InstanceRows {
		want := workloadid.NewWorkloadInstanceID(row.RepoID, row.WorkloadName, row.Environment).String()
		if row.InstanceID != want {
			t.Errorf("InstanceRow.InstanceID = %q, constructor recomputes %q", row.InstanceID, want)
		}
	}
}

// A blank environment segment must never mint any identifier: the
// constructor returns the empty id, and the emission sites drop the row
// instead of appending it. Emitting it would MERGE every affected candidate
// onto one shared empty-id node -- a broader collision than the bare-prefix
// node the pre-refactor inline format string produced
// ("workload-instance:<name>:") (#6580 P1).
func TestProjectionBlankEnvironmentDropsInstanceRow(t *testing.T) {
	t.Parallel()
	candidates := []WorkloadCandidate{
		{
			RepoID:        "repo:alpha",
			RepoName:      "checkout",
			ResourceKinds: []string{"Deployment", "Service"},
			Confidence:    0.98,
			Provenance:    []string{"k8s_resource"},
		},
	}
	result := BuildProjectionRows(candidates, map[string][]string{"repo:alpha": {""}})
	if len(result.InstanceRows) != 0 {
		t.Fatalf("InstanceRows len = %d, want 0: blank environments must not emit rows", len(result.InstanceRows))
	}
}

// The provisioned-platforms path builds RuntimePlatformRows in
// provisionedRuntimePlatformRows, outside the WorkloadRows/InstanceRows
// coverage above. Every such row must equal the constructor recomputed from
// the row's own repo and environment (#6580 P2): under the re-key, passing
// the wrong repository here would key a second node for the same instance.
func TestProvisionedRuntimePlatformRowsMatchConstructors(t *testing.T) {
	t.Parallel()
	candidates := []WorkloadCandidate{
		{
			RepoID:              "repo-service",
			RepoName:            "service-api",
			DeploymentRepoID:    "repo-payments",
			ProvisioningRepoIDs: []string{"repo-infra"},
			ProvisioningEvidenceKinds: map[string][]string{
				"repo-infra": {TerraformPlatformEvidenceKind("ecs", "service")},
			},
			Classification: "service",
			Confidence:     0.96,
			Provenance:     []string{"dockerfile_runtime"},
		},
	}
	deploymentEnvs := map[string][]string{
		"repo-payments": {"prod"},
		"repo-infra":    {"prod", "qa"},
	}
	infraPlatforms := map[string][]InfrastructurePlatformRow{
		"repo-infra": {
			{
				PlatformID:       "platform:ecs:aws:cluster/runtime-main:none:none",
				PlatformName:     "runtime-main",
				PlatformKind:     "ecs",
				PlatformProvider: "aws",
				PlatformLocator:  "cluster/runtime-main",
			},
		},
	}
	result := BuildProjectionRowsWithInfrastructurePlatforms(candidates, deploymentEnvs, infraPlatforms)
	if len(result.RuntimePlatformRows) == 0 {
		t.Fatal("expected runtime platform rows, got none: vacuous pass guard")
	}
	// The fixture sets no WorkloadName, so candidateWorkloadName falls back
	// to the trimmed RepoName; the pin below covers repo+env routing and
	// format tracking, not name derivation.
	for _, row := range result.RuntimePlatformRows {
		want := workloadid.NewWorkloadInstanceID(row.RepoID, "service-api", row.Environment).String()
		if row.InstanceID != want {
			t.Errorf("RuntimePlatformRow.InstanceID = %q, constructor recomputes %q (env %q)", row.InstanceID, want, row.Environment)
		}
	}
}
