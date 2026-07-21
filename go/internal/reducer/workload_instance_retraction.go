// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"slices"
	"strings"
)

// ExistingWorkloadInstance is one WorkloadInstance node already materialized in
// the canonical graph for a repository owned by the current materialization
// pass and tagged with this handler's evidence source.
type ExistingWorkloadInstance struct {
	RepoID     string
	InstanceID string
}

// WorkloadInstanceRetractionLookup resolves WorkloadInstance nodes already
// materialized in the graph for the repositories in the current
// materialization pass, scoped to this handler's evidence source. It mirrors
// WorkloadDependencyGraphLookup.ListWorkloadDependencyEdges: a repo-owned,
// evidence-source-scoped read of current graph truth, so retraction can never
// cross repository or evidence-source ownership boundaries.
type WorkloadInstanceRetractionLookup interface {
	ListWorkloadInstances(
		ctx context.Context,
		repoIDs []string,
		evidenceSource string,
	) ([]ExistingWorkloadInstance, error)
}

// currentInstanceRetractionRepoIDs returns the distinct, sorted repository ids
// that own a workload in the current materialization pass. It mirrors the
// currentRepoIDs derivation in ReconcileWorkloadDependencyEdges so instance
// retraction is scoped to exactly the same repositories as the sibling
// workload-dependency-edge reconciliation, and can never widen to a repository
// or scope this pass did not materialize.
func currentInstanceRetractionRepoIDs(descriptors []RepoDescriptor) []string {
	seen := make(map[string]struct{}, len(descriptors))
	repoIDs := make([]string, 0, len(descriptors))
	for _, descriptor := range descriptors {
		repoID := strings.TrimSpace(descriptor.RepoID)
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		seen[repoID] = struct{}{}
		repoIDs = append(repoIDs, repoID)
	}
	slices.Sort(repoIDs)
	return repoIDs
}

// currentInstanceIDsByRepo groups this generation's written WorkloadInstance
// ids by owning repository. It is the positive-evidence signal
// ReconcileWorkloadInstanceRetraction requires before treating a repo's
// existing instances as candidates for retraction: RepoDescriptors is
// populated per candidate BEFORE the per-candidate environment-resolution loop
// that produces InstanceRows (BuildProjectionRowsWithInfrastructurePlatforms
// in projection.go appends the descriptor at candidate-scope, then appends
// instance rows only for environments that actually resolved). A repository
// can therefore legitimately have a descriptor this pass while contributing
// ZERO instance rows -- for example a transient gap in any of the seven
// environment-alias evidence classes (path overlay, namespace fallback,
// artifact path token, CI observation, cloud tag, operator declared, hostname
// inference). That is "environment unresolved this pass", not "workload
// genuinely absent", and MUST NOT be read as supersession of every instance
// the repo has ever had. Only a repo with a non-empty entry here supplies
// positive evidence that its instance set was actually re-produced this pass.
func currentInstanceIDsByRepo(instanceRows []InstanceRow) map[string]map[string]struct{} {
	byRepo := make(map[string]map[string]struct{})
	for _, row := range instanceRows {
		repoID := strings.TrimSpace(row.RepoID)
		id := strings.TrimSpace(row.InstanceID)
		if repoID == "" || id == "" {
			continue
		}
		ids, ok := byRepo[repoID]
		if !ok {
			ids = make(map[string]struct{})
			byRepo[repoID] = ids
		}
		ids[id] = struct{}{}
	}
	return byRepo
}

// ReconcileWorkloadInstanceRetraction computes which previously materialized
// WorkloadInstance ids are superseded by this generation's projection: owned by
// a repository in the current materialization pass, tagged with this handler's
// evidence source, absent from the instance ids that repository wrote this
// pass, AND — the positive-evidence guard — that repository must have written
// at least one instance row this pass. A repository with a descriptor but zero
// instance rows this pass supplies no positive evidence that its workload's
// environments actually changed (see currentInstanceIDsByRepo); its existing
// instances are left untouched rather than treated as bulk-superseded.
//
// This closes the gap the environment-alias contract (#5473) introduced: once
// ExtractOverlayEnvironments started canonicalizing environment names (for
// example "production" -> "prod"), the durable instance id
// (workload-instance:<workload_name>:<environment>) changed for any repo whose
// overlay used a pre-canonical alias or mixed case. Because WorkloadInstance
// writes are MERGE-only (see batchWorkloadInstanceNodeUpsertCypher), the old
// key would otherwise survive forever alongside the new canonical key, along
// with its INSTANCE_OF, DEPLOYMENT_SOURCE, and RUNS_ON edges — duplicate
// deployment and runtime truth. The retract statement DETACH DELETEs the
// instance node, which destroys EVERY relationship incident to it as
// collateral — including any USES->CloudResource edge — regardless of which
// domain owns that edge type. USES edges have their own independent
// scope-retraction pass elsewhere (workload_cloud_relationship_materialization.go);
// that pass is not made redundant by this one, and a USES edge disappearing
// here is a side effect of its owning node being gone, not a decision this
// reconciliation makes about USES edges directly.
//
// descriptors and instanceRows MUST come from the same ProjectionResult so the
// repository scope and the current-generation instance-id set describe the
// same materialization pass; passing mismatched inputs risks retracting
// instances a different pass still owns.
//
// The returned repoIDs is the exact repository scope used to compute
// superseded and MUST be threaded to the delete-time predicate (see
// WorkloadMaterializer.RetractInstances) so a stale decision computed here can
// never delete a node a concurrent write has since re-owned under a different
// repository or evidence source.
func ReconcileWorkloadInstanceRetraction(
	ctx context.Context,
	descriptors []RepoDescriptor,
	instanceRows []InstanceRow,
	lookup WorkloadInstanceRetractionLookup,
) (repoIDs []string, superseded []string, err error) {
	if len(descriptors) == 0 || lookup == nil {
		return nil, nil, nil
	}
	repoIDs = currentInstanceRetractionRepoIDs(descriptors)
	if len(repoIDs) == 0 {
		return nil, nil, nil
	}

	existing, err := lookup.ListWorkloadInstances(ctx, repoIDs, EvidenceSourceWorkloads)
	if err != nil {
		return repoIDs, nil, err
	}
	if len(existing) == 0 {
		return repoIDs, nil, nil
	}

	// Defense in depth: re-check every returned row's RepoID against the exact
	// repository set this pass materialized, mirroring
	// currentWorkloadDependencyRepoIDs in workload_dependency_reconciliation.go.
	// A Lookup implementation is expected to filter by repoIDs itself (see
	// neo4jWorkloadInstanceRetractionLookup), but retraction is destructive, so
	// this function does not trust that filter alone — an over-broad Lookup can
	// never widen what gets retracted here.
	inScope := make(map[string]struct{}, len(repoIDs))
	for _, repoID := range repoIDs {
		inScope[repoID] = struct{}{}
	}

	currentByRepo := currentInstanceIDsByRepo(instanceRows)

	seen := make(map[string]struct{}, len(existing))
	superseded = make([]string, 0, len(existing))
	for _, instance := range existing {
		id := strings.TrimSpace(instance.InstanceID)
		if id == "" {
			continue
		}
		repoID := strings.TrimSpace(instance.RepoID)
		if _, ok := inScope[repoID]; !ok {
			continue
		}
		// Positive-evidence guard (see currentInstanceIDsByRepo): a repo that
		// produced zero instance rows this pass gives no signal that its
		// workload's environments actually changed, so none of its existing
		// instances may be treated as superseded.
		currentIDs, hasPositiveEvidence := currentByRepo[repoID]
		if !hasPositiveEvidence {
			continue
		}
		if _, ok := currentIDs[id]; ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		superseded = append(superseded, id)
	}
	slices.Sort(superseded)
	return repoIDs, superseded, nil
}
