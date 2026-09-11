// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// DeploymentEvidenceStore is the narrow optional port a
// ContentStore implements to answer deployment-evidence reads directly. It
// is the structural twin of the staying root package's unexported
// repositoryDeploymentEvidenceReadModelStore: the ContentReader satisfies
// both, so the fast-path assertion below resolves exactly as it did before
// the move (#6060, lane B B3).
type DeploymentEvidenceStore interface {
	RepositoryDeploymentEvidence(context.Context, string) (querycontract.RepositoryDeploymentEvidenceReadModel, error)
}

// LoadRepositoryDeploymentEvidence returns the Postgres read-model fast
// path for repoID when the content store can answer it directly, binding
// each cross-repo evidence artifact to the caller's grant before building
// the evidence map. Root keeps an unexported forwarder so its read-model
// tripwires compile unchanged.
func LoadRepositoryDeploymentEvidence(ctx context.Context, content querycontract.ContentStore, repoID string) (map[string]any, error) {
	store, ok := content.(DeploymentEvidenceStore)
	if !ok || repoID == "" {
		return nil, nil
	}
	readModel, err := store.RepositoryDeploymentEvidence(ctx, repoID)
	if err != nil {
		return nil, err
	}
	if !readModel.Available || len(readModel.Rows) == 0 {
		return nil, nil
	}
	// #5167 W3 P0: bind each cross-repo evidence artifact to the caller's grant
	// before building the evidence map (same shared filter the graph-traversal
	// path in queryRepoDeploymentEvidence applies) so a scoped caller never sees
	// a cross-tenant repository on the non-anchor endpoint. Dropping cross-tenant
	// rows never makes the set MORE truncated -- an untruncated read-model stays
	// the complete in-grant set -- so readModel.Truncated is carried through
	// unchanged.
	filteredRows := filterDeploymentEvidenceRowsForAccess(readModel.Rows, repoID, querycontract.RepositoryAccessFilterFromContext(ctx))
	if len(filteredRows) == 0 {
		return nil, nil
	}
	result := buildGraphDeploymentEvidence(filteredRows)
	result["artifact_limit"] = readModel.Limit
	result["artifacts_truncated"] = readModel.Truncated
	return result, nil
}

// attachRepositoryObservationIdentity and queryRepoDeploymentEvidence keep the
// in-package spelling after the #6060 export; root stayers name the exported
// spelling.
func attachRepositoryObservationIdentity(artifact, row map[string]any, endpoint string) {
	AttachRepositoryObservationIdentity(artifact, row, endpoint)
}

func queryRepoDeploymentEvidence(ctx context.Context, reader querycontract.GraphQuery, content querycontract.ContentStore, params map[string]any) (map[string]any, error) {
	return QueryRepoDeploymentEvidence(ctx, reader, content, params)
}

// buildGraphDeploymentEvidence keeps the in-package spelling after the #6060
// export; root tests name BuildGraphDeploymentEvidence.
func buildGraphDeploymentEvidence(rows []map[string]any) map[string]any {
	return BuildGraphDeploymentEvidence(rows)
}
