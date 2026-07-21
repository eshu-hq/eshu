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

// currentInstanceIDs returns the distinct instance ids this generation's
// projection wrote to WorkloadInstance nodes.
func currentInstanceIDs(instanceRows []InstanceRow) []string {
	seen := make(map[string]struct{}, len(instanceRows))
	ids := make([]string, 0, len(instanceRows))
	for _, row := range instanceRows {
		id := strings.TrimSpace(row.InstanceID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// ReconcileWorkloadInstanceRetraction computes which previously materialized
// WorkloadInstance ids are superseded by this generation's projection: owned by
// a repository in the current materialization pass, tagged with this handler's
// evidence source, but absent from the instance ids this pass just wrote.
//
// This closes the gap the environment-alias contract (#5473) introduced: once
// ExtractOverlayEnvironments started canonicalizing environment names (for
// example "production" -> "prod"), the durable instance id
// (workload-instance:<workload_name>:<environment>) changed for any repo whose
// overlay used a pre-canonical alias or mixed case. Because WorkloadInstance
// writes are MERGE-only (see batchWorkloadInstanceNodeUpsertCypher), the old
// key would otherwise survive forever alongside the new canonical key, along
// with its INSTANCE_OF, DEPLOYMENT_SOURCE, and RUNS_ON edges — duplicate
// deployment and runtime truth. USES edges are already scope-retracted
// elsewhere (workload_cloud_relationship_materialization.go) and are untouched
// by this reconciliation.
//
// descriptors and instanceRows MUST come from the same ProjectionResult so the
// repository scope and the current-generation instance-id set describe the
// same materialization pass; passing mismatched inputs risks retracting
// instances a different pass still owns.
func ReconcileWorkloadInstanceRetraction(
	ctx context.Context,
	descriptors []RepoDescriptor,
	instanceRows []InstanceRow,
	lookup WorkloadInstanceRetractionLookup,
) ([]string, error) {
	if len(descriptors) == 0 || lookup == nil {
		return nil, nil
	}
	repoIDs := currentInstanceRetractionRepoIDs(descriptors)
	if len(repoIDs) == 0 {
		return nil, nil
	}

	existing, err := lookup.ListWorkloadInstances(ctx, repoIDs, EvidenceSourceWorkloads)
	if err != nil {
		return nil, err
	}
	if len(existing) == 0 {
		return nil, nil
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

	current := make(map[string]struct{}, len(instanceRows))
	for _, id := range currentInstanceIDs(instanceRows) {
		current[id] = struct{}{}
	}

	seen := make(map[string]struct{}, len(existing))
	superseded := make([]string, 0, len(existing))
	for _, instance := range existing {
		id := strings.TrimSpace(instance.InstanceID)
		if id == "" {
			continue
		}
		if _, ok := inScope[strings.TrimSpace(instance.RepoID)]; !ok {
			continue
		}
		if _, ok := current[id]; ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		superseded = append(superseded, id)
	}
	slices.Sort(superseded)
	return superseded, nil
}
