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
// inputs — so current routing proof is TestProjectionBlankEnvironment-
// YieldsEmptyInstanceID plus `rg -n 'Sprintf\(\"workload:' internal/reducer/`
// whose only remaining hits are `workload:a->b` partition keys, not
// identifier construction sites.
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

// A blank environment segment must never mint a bare-prefix identifier that
// MERGEs unrelated candidates onto one shared node. The constructor returns
// the empty id; the pre-refactor inline format string emitted
// "workload-instance:<name>:" instead.
func TestProjectionBlankEnvironmentYieldsEmptyInstanceID(t *testing.T) {
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
	if len(result.InstanceRows) != 1 {
		t.Fatalf("InstanceRows len = %d, want 1: vacuous pass guard", len(result.InstanceRows))
	}
	if got := result.InstanceRows[0].InstanceID; got != "" {
		t.Fatalf("InstanceID = %q, want empty id for a blank environment", got)
	}
}
