// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

// BuildDeployableUnitCorrelationIncidentRepos builds the gap-reconciliation
// probe: which of repoIDs still own an outgoing deployable-unit correlation
// edge. Callers treat an admitted anchor absent from the result as a gap and
// delete its fingerprint rows so the next maintenance pass re-opens it.
// BuildRetractDeployableUnitCorrelationEdges deletes deployable-unit
// correlation edges for repository acceptance units owned by one evidence
// source.
func BuildRetractDeployableUnitCorrelationEdges(repoIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    node.RetractDeployableUnitCorrelationEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}

func BuildDeployableUnitCorrelationIncidentRepos(repoIDs []string) Statement {
	return Statement{
		Operation: OperationCanonicalProbe,
		Cypher:    node.DeployableUnitCorrelationIncidentReposCypher,
		Parameters: map[string]any{
			"repo_ids": repoIDs,
		},
	}
}
