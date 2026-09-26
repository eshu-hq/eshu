// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicecatalog

import (
	"context"
	"fmt"
)

// ServiceCatalogCorrelationResolutionNotReadyFailureClass classifies a
// deferral of a service catalog correlation intent whose service deployment
// and dependency evidence would be read while the relationship corpus fence is
// open: some active scope's current relationship generation is
// retired-or-pending, so the by-repos resolved read omits that scope's rows.
//
// Registered as a non-counting reducer retry class
// (nonCountingReducerRetryFailureClasses in
// go/internal/storage/postgres/reducer_queue_readiness_sql.go): the intent is
// waiting on upstream resolution, not failing on its own merits. Without this
// gate the handler committed a service materialization generation from the
// partial set, superseding the prior generation and reporting spurious removed
// deployment and dependency evidence on changed-since; the only producer of
// these intents never reopens them when the foreign scope completes (#7258).
const ServiceCatalogCorrelationResolutionNotReadyFailureClass = "service_catalog_correlation_resolution_not_ready"

// serviceCatalogCorrelationResolutionNotReadyError defers the whole intent,
// before any correlation or materialization write, until the relationship
// corpus fence closes.
type serviceCatalogCorrelationResolutionNotReadyError struct {
	scopeID      string
	generationID string
}

func (e serviceCatalogCorrelationResolutionNotReadyError) Error() string {
	return fmt.Sprintf(
		"relationship corpus fence open (an active scope's relationship generation is retired or pending) for service catalog scope %s generation %s; deferring service catalog correlation rather than materializing against a partial resolved set",
		e.scopeID,
		e.generationID,
	)
}

// Retryable reports that the deferral must be retried once the fence closes.
func (serviceCatalogCorrelationResolutionNotReadyError) Retryable() bool { return true }

// FailureClass returns the non-counting readiness class for the queue.
func (serviceCatalogCorrelationResolutionNotReadyError) FailureClass() string {
	return ServiceCatalogCorrelationResolutionNotReadyFailureClass
}

// attachServiceRelationshipEvidence loads the resolved cross-repo relationships
// for the correlated services' repositories once, through the corpus-fenced
// read, and attaches BOTH the deployment (#1985) and dependencies (#1987)
// evidence families to the matching per-service writes. Both families share
// the same resolved_relationships source and a single bounded load; the build
// helpers partition the loaded set by relationship type so neither family
// admits the other's edges.
//
// Handle calls it before any write. An open corpus fence returns
// serviceCatalogCorrelationResolutionNotReadyError so the intent defers with
// nothing written; a read failure is an ordinary error. It is a no-op (no
// read, no fence) when no loader is wired, no service is being materialized,
// or no decision carries a repository, so both families stay purely additive.
func (h ServiceCatalogCorrelationHandler) attachServiceRelationshipEvidence(
	ctx context.Context,
	scopeID string,
	generationID string,
	writes []ServiceMaterializationWrite,
	decisions []ServiceCatalogCorrelationDecision,
) error {
	if h.DeploymentRelationshipLoader == nil || len(writes) == 0 {
		return nil
	}
	repoByService := serviceRepositoryIndex(decisions)
	repoIDs := distinctServiceRepositoryIDs(writes, repoByService)
	if len(repoIDs) == 0 {
		return nil
	}
	resolved, complete, err := h.DeploymentRelationshipLoader.GetResolvedRelationshipsForReposWithCorpusFence(ctx, repoIDs)
	if err != nil {
		return fmt.Errorf("load service deployment and dependency relationships: %w", err)
	}
	if !complete {
		return serviceCatalogCorrelationResolutionNotReadyError{scopeID: scopeID, generationID: generationID}
	}
	deploymentByRepo := groupDeploymentRelationshipsByRepo(resolved)
	dependencyByRepo := groupDependencyRelationshipsByRepo(resolved)
	for i := range writes {
		repoID := repoByService[writes[i].ServiceID]
		if repoID == "" {
			continue
		}
		writes[i].Deployment = buildServiceDeploymentEvidence(deploymentByRepo[repoID])
		writes[i].Dependencies = buildServiceDependencyEvidence(dependencyByRepo[repoID])
	}
	return nil
}
