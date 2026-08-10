// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"strings"
)

// SupplyChainRuntimeContext is one repository's read-time-resolved runtime
// context: the workloads, services, deployments, environments, and catalog
// refs that repository currently maps to, resolved from active
// workload_identity, service_catalog_correlation, platform_materialization,
// and deployment-correlation facts at query time (issue #5746).
//
// Read-time resolution is deliberate: a finding's baked payload fields are
// stamped at reduce time and go stale the moment runtime reality changes
// (redeploy, delete, promote), while this join reads the CURRENT facts on
// every request. Absence of a workload here is an honest "current state of
// knowledge" that self-heals on the next read — no readiness gate, no
// re-enqueue, no fan-out.
type SupplyChainRuntimeContext struct {
	WorkloadIDs   []string
	ServiceIDs    []string
	DeploymentIDs []string
	Environments  []string
	// EnvironmentEvidence records the strongest corroboration state for each
	// environment resolved from current accepted CI/CD correlation facts.
	EnvironmentEvidence map[string]string
	CatalogEntityRefs   []string
	CatalogOwnerRefs    []string
}

// SupplyChainRuntimeContextResult is the response-side envelope attached to
// one impact finding as `runtime_context` (#5746). TruthBasis labels the
// resolution path so a caller cannot mistake these IDs for baked payload
// fields. The workload_id/service_id/environment filters resolve the same
// current repository mappings independently (#5747).
type SupplyChainRuntimeContextResult struct {
	// TruthBasis is always "read_time_resolved": the context was resolved
	// from the repository's active runtime facts at query time, not baked
	// into the finding at reduce time. Empty lists are an honest "no runtime
	// facts landed yet" (fresh ingest) that self-heals on the next read.
	TruthBasis    string   `json:"truth_basis"`
	WorkloadIDs   []string `json:"workload_ids,omitempty"`
	ServiceIDs    []string `json:"service_ids,omitempty"`
	DeploymentIDs []string `json:"deployment_ids,omitempty"`
	Environments  []string `json:"environments,omitempty"`
	// EnvironmentEvidence records the strongest corroboration state for each
	// environment resolved from current accepted CI/CD correlation facts.
	// Values use the existing deploy_event/declared vocabulary; deploy_event
	// wins when multiple active facts name the same environment.
	EnvironmentEvidence map[string]string `json:"environment_evidence,omitempty"`
	CatalogEntityRefs   []string          `json:"catalog_entity_refs,omitempty"`
	CatalogOwnerRefs    []string          `json:"catalog_owner_refs,omitempty"`
}

const supplyChainRuntimeContextTruthBasis = "read_time_resolved"

// supplyChainImpactRuntimeContextReader is the optional store capability the
// findings handler type-asserts on its ImpactFindings store to resolve
// per-repository runtime context at read time. A store that does not
// implement it (legacy, test double) leaves rows untouched — the response
// degrades to the pre-#5746 shape rather than erroring. The final two slices
// carry scoped-token repository and ingestion-scope grants; both empty means
// unrestricted, matching the runtime-filter SQL contract.
type supplyChainImpactRuntimeContextReader interface {
	ListSupplyChainImpactRuntimeContext(
		context.Context,
		[]string,
		[]string,
		[]string,
	) (map[string]SupplyChainRuntimeContext, error)
}

// applySupplyChainRuntimeContext resolves each finding row's runtime context
// (workloads, services, deployments, environments, catalog refs) from its
// repository_id at QUERY time and attaches it to the row as
// RuntimeContext — read-time resolution instead of reduce-time baking
// (issue #5746; reduce-time baking was disproven because the workload facts
// commit seconds after the finding and nothing re-triggers it).
//
// Rows with no repository anchor are skipped untouched. Rows whose repository
// has no runtime facts yet (fresh ingest) get an honest empty, labeled
// context — absence is "current state of knowledge", not an error, and it
// self-heals on the next read. The probe NEVER writes the resolved IDs into
// the baked WorkloadIDs/ServiceIDs/Environments fields; #5747 makes filters
// consult current active runtime facts directly instead.
//
// A reader error is propagated (the caller maps graph sentinels to the
// bounded retryable envelope and everything else to a plain 500) rather than
// serving an empty context that could be misread as "nothing runs this",
// matching the cloud-runtime probe's fail-loud contract. Scoped access is
// forwarded to the reader so response hydration and runtime filters cannot
// disagree about which current facts the caller may observe.
func (h *SupplyChainHandler) applySupplyChainRuntimeContext(
	ctx context.Context,
	rows []SupplyChainImpactFindingRow,
	access repositoryAccessFilter,
) error {
	if h == nil || len(rows) == 0 {
		return nil
	}
	reader, ok := h.ImpactFindings.(supplyChainImpactRuntimeContextReader)
	if !ok || reader == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(rows))
	repositoryIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		repositoryID := strings.TrimSpace(row.RepositoryID)
		if repositoryID == "" {
			continue
		}
		if _, dup := seen[repositoryID]; dup {
			continue
		}
		seen[repositoryID] = struct{}{}
		repositoryIDs = append(repositoryIDs, repositoryID)
	}
	if len(repositoryIDs) == 0 {
		return nil
	}
	var allowedRepositoryIDs, allowedScopeIDs []string
	if access.scoped() {
		allowedRepositoryIDs = access.allowedRepositoryIDs
		allowedScopeIDs = access.allowedScopeIDs
	}
	byRepo, err := reader.ListSupplyChainImpactRuntimeContext(
		ctx,
		repositoryIDs,
		allowedRepositoryIDs,
		allowedScopeIDs,
	)
	if err != nil {
		return err
	}
	for i := range rows {
		repositoryID := strings.TrimSpace(rows[i].RepositoryID)
		if repositoryID == "" {
			continue
		}
		resolved := byRepo[repositoryID]
		environments := sortedUniqueNonEmptyStrings(resolved.Environments)
		rows[i].RuntimeContext = &SupplyChainRuntimeContextResult{
			TruthBasis:    supplyChainRuntimeContextTruthBasis,
			WorkloadIDs:   sortedUniqueNonEmptyStrings(resolved.WorkloadIDs),
			ServiceIDs:    sortedUniqueNonEmptyStrings(resolved.ServiceIDs),
			DeploymentIDs: sortedUniqueNonEmptyStrings(resolved.DeploymentIDs),
			Environments:  environments,
			EnvironmentEvidence: cloneSupplyChainRuntimeEnvironmentEvidence(
				resolved.EnvironmentEvidence,
				environments,
			),
			CatalogEntityRefs: sortedUniqueNonEmptyStrings(resolved.CatalogEntityRefs),
			CatalogOwnerRefs:  sortedUniqueNonEmptyStrings(resolved.CatalogOwnerRefs),
		}
	}
	return nil
}
