// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

// CanonicalRepoRelationshipParams holds the parameters for a typed repository
// relationship upsert.
type CanonicalRepoRelationshipParams struct {
	RepoID           string
	TargetRepoID     string
	RelationshipType string
	EvidenceType     string
	ResolvedID       string
	GenerationID     string
	EvidenceCount    int
	EvidenceKinds    []string
	ResolutionSource string
	Confidence       float64
	Rationale        string
	// SourceTool is the normalized provenance token for the producing tool
	// (#3997/#3999), derived from the edge's primary evidence kind. Empty when no
	// tool is derivable; never a guessed value.
	SourceTool string
	// SourceRevision is the declared git revision (branch/tag/SHA) an ArgoCD
	// deployment source targets (#5441). Empty when the evidence family does
	// not carry a source revision (e.g. Terraform module edges).
	//
	// A third field, DestinationNamespace, was deliberately removed before
	// merge (#5441 review round 2): it has no evidence producer on any of
	// the five widened relationship types (see the Candidate doc comment in
	// go/internal/relationships/models.go), so it would have shipped as a
	// permanently-empty property with no producer.
	SourceRevision string
	// FirstPartyRefVersion is the pinned module/reference version extracted
	// from a raw first-party source string (#5441), e.g. the `ref=` query
	// parameter on a Terraform/Terragrunt git-sourced module. Empty when the
	// evidence carries no pin.
	FirstPartyRefVersion string
}

// CanonicalRunsOnParams holds the parameters for a repository-scoped RUNS_ON
// upsert that resolves to workload instances.
type CanonicalRunsOnParams struct {
	RepoID     string
	PlatformID string
}

// BuildCanonicalRepoRelationshipUpsert builds a typed repository relationship statement.
func BuildCanonicalRepoRelationshipUpsert(p CanonicalRepoRelationshipParams, evidenceSource string) Statement {
	cypher := canonicalTypedRepoRelationshipUpsertCypher(p.RelationshipType)
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    cypher,
		Parameters: map[string]any{
			"repo_id":                 p.RepoID,
			"target_repo_id":          p.TargetRepoID,
			"relationship_type":       p.RelationshipType,
			"evidence_type":           p.EvidenceType,
			"evidence_source":         evidenceSource,
			"resolved_id":             p.ResolvedID,
			"generation_id":           p.GenerationID,
			"evidence_count":          p.EvidenceCount,
			"evidence_kinds":          p.EvidenceKinds,
			"resolution_source":       p.ResolutionSource,
			"confidence":              RepoRelationshipConfidence(p.Confidence),
			"rationale":               p.Rationale,
			"source_tool":             p.SourceTool,
			"source_revision":         p.SourceRevision,
			"first_party_ref_version": p.FirstPartyRefVersion,
		},
	}
}

func RepoRelationshipConfidence(value float64) float64 {
	if value <= 0 {
		return 0.9
	}
	return value
}

func canonicalTypedRepoRelationshipUpsertCypher(relationshipType string) string {
	switch relationshipType {
	case string(edgetype.DeploysFrom):
		return node.CanonicalDeploysFromRepoRelationshipUpsertCypher
	case string(edgetype.DiscoversConfigIn):
		return node.CanonicalDiscoversConfigInRepoRelationshipUpsertCypher
	case string(edgetype.ProvisionsDependencyFor):
		return node.CanonicalProvisionsDependencyForRepoRelationshipUpsertCypher
	case string(edgetype.UsesModule):
		return node.CanonicalUsesModuleRepoRelationshipUpsertCypher
	case string(edgetype.ReadsConfigFrom):
		return node.CanonicalReadsConfigFromRepoRelationshipUpsertCypher
	default:
		return CanonicalRepoDependencyUpsertCypher
	}
}

func BatchCanonicalTypedRepoRelationshipUpsertCypher(relationshipType string) (string, bool) {
	switch relationshipType {
	case string(edgetype.DeploysFrom):
		return node.BatchCanonicalDeploysFromRepoRelationshipUpsertCypher, true
	case string(edgetype.DiscoversConfigIn):
		return node.BatchCanonicalDiscoversConfigInRepoRelationshipUpsertCypher, true
	case string(edgetype.ProvisionsDependencyFor):
		return node.BatchCanonicalProvisionsDependencyForRepoRelationshipUpsertCypher, true
	case string(edgetype.UsesModule):
		return node.BatchCanonicalUsesModuleRepoRelationshipUpsertCypher, true
	case string(edgetype.ReadsConfigFrom):
		return node.BatchCanonicalReadsConfigFromRepoRelationshipUpsertCypher, true
	default:
		return "", false
	}
}

// BuildCanonicalRunsOnUpsert builds a repository-scoped RUNS_ON statement.
func BuildCanonicalRunsOnUpsert(p CanonicalRunsOnParams, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    node.CanonicalRunsOnUpsertCypher,
		Parameters: map[string]any{
			"repo_id":         p.RepoID,
			"platform_id":     p.PlatformID,
			"evidence_source": evidenceSource,
		},
	}
}

// BuildEdgeRouteStatements removes pre-upgrade propertyless RUNS_ON identities
// before issuing deterministic keyed MERGEs. Workload dependency upgrades use
// the domain's existing retract-then-replay boundary; keeping migration work
// out of its hot writer avoids an unbounded relationship scan on every batch.
func BuildEdgeRouteStatements(cypher string, rows []map[string]any, batchSize int) []Statement {
	statements := make([]Statement, 0, 2)
	switch cypher {
	case node.BatchCanonicalRunsOnUpsertCypher:
		statements = append(
			statements,
			BuildBatchedStatements(node.BatchCanonicalRunsOnLegacyIdentityCleanupCypher, rows, batchSize)...,
		)
	}
	return append(statements, BuildBatchedStatements(cypher, rows, batchSize)...)
}
