// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// TerraformStateOwnershipResolver resolves the config repository that owns a
// Terraform state backend, mirroring
// tfstatebackend.Resolver.ResolveConfigCommitForBackend's own selection rule
// (single distinct owning repo, ambiguous or absent ownership both resolve
// to "not found"). Defined here rather than importing
// internal/relationships/tfstatebackend directly so this package depends on
// a narrow port, not the resolver's full Postgres-backed implementation;
// production wiring (cmd/projector) adapts *tfstatebackend.Resolver to this
// interface.
type TerraformStateOwnershipResolver interface {
	// ResolveOwningRepoID returns the single config repo ID that owns the
	// given (backend_kind, locator_hash) pair, and false when no repo or more
	// than one distinct repo claims it. Never guesses: an ambiguous or absent
	// resolution must return false, ok, never a best-effort repo ID.
	ResolveOwningRepoID(ctx context.Context, backendKind, locatorHash string) (repoID string, ok bool)
}

// canonicalTerraformStateMatchesConfigEdgeCypher writes the #5443
// MATCHES_STATE edge from a config-declared TerraformResource to the
// TerraformStateResource it matches by EXACT address equality, never
// normalized. An expanded state address from a module/count/for_each
// instance (e.g. `module.vpc.aws_instance.foo["us-east-1"]`) legitimately
// does not equal its bare config address (`aws_instance.foo`) and stays
// applied-only rather than being force-matched -- that is the honest,
// documented limitation this issue settles on, not a bug. Both anchors are
// indexed: `s` on TerraformStateResource.uid (uid uniqueness constraint) and
// `c` on TerraformResource.name (tf_resource_name index, graph/schema_tables.go),
// filtered further by repo_id to stay within the owning repo.
const canonicalTerraformStateMatchesConfigEdgeCypher = `UNWIND $rows AS row
MATCH (c:TerraformResource {repo_id: row.owning_repo_id, name: row.address})
MATCH (s:TerraformStateResource {uid: row.uid})
MERGE (c)-[e:MATCHES_STATE]->(s)
SET e.evidence_source = 'projector/tfstate',
    e.generation_id = row.generation_id`

// resolveTerraformStateOwnership enriches rows with OwningRepoID before the
// pure Cypher builders run. Memoized per distinct (backend_kind,
// locator_hash) pair within this batch -- one resolver call per distinct
// backend, not one per resource -- mirroring the memoization already used
// for the equivalent drift-correlation lookup
// (incident_repository_correlation_build.go). A nil resolver (the default;
// see WithTerraformStateOwnershipResolver) or a row with a blank backend
// identity leaves OwningRepoID empty, which downstream (config_repo_id node
// property, terraformStateMatchesConfigEdgeStatements) is the honest
// "ownership not resolved" state, never a guess.
func (w *CanonicalNodeWriter) resolveTerraformStateOwnership(
	ctx context.Context,
	rows []projector.TerraformStateResourceRow,
) []projector.TerraformStateResourceRow {
	if w.tfStateOwnershipResolver == nil || len(rows) == 0 {
		return rows
	}

	type ownerKey struct{ backendKind, locatorHash string }
	memo := make(map[ownerKey]string, len(rows))
	out := make([]projector.TerraformStateResourceRow, len(rows))
	for i, row := range rows {
		out[i] = row
		if row.BackendKind == "" || row.LocatorHash == "" {
			continue
		}
		key := ownerKey{row.BackendKind, row.LocatorHash}
		repoID, cached := memo[key]
		if !cached {
			if resolved, ok := w.tfStateOwnershipResolver.ResolveOwningRepoID(ctx, row.BackendKind, row.LocatorHash); ok {
				repoID = resolved
			}
			memo[key] = repoID
		}
		out[i].OwningRepoID = repoID
	}
	return out
}

// terraformStateMatchesConfigEdgeStatements builds the batched MATCHES_STATE
// edge write for every row whose ownership resolved (OwningRepoID
// non-empty) and whose address is non-blank. Rows with no resolved owner are
// silently excluded from this statement -- not an error, since "ownership
// unresolved" is a legitimate, expected state (see
// TerraformStateOwnershipResolver) recorded on the node itself via
// config_repo_id (canonicalTerraformStateResourceUpsertCypher) rather than
// this edge phase.
func (w *CanonicalNodeWriter) terraformStateMatchesConfigEdgeStatements(mat projector.CanonicalMaterialization) []Statement {
	rows := make([]map[string]any, 0, len(mat.TerraformStateResources))
	for _, row := range mat.TerraformStateResources {
		if row.OwningRepoID == "" || row.Address == "" {
			continue
		}
		rows = append(rows, map[string]any{
			"uid":            row.UID,
			"address":        row.Address,
			"owning_repo_id": row.OwningRepoID,
			"generation_id":  mat.GenerationID,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	return tfstateBatchedStatements(
		canonicalTerraformStateMatchesConfigEdgeCypher,
		rows,
		w.batchSize,
		"MATCHES_STATE",
		mat,
	)
}

// terraformStateOwningRepoIDValue converts an empty OwningRepoID to a Cypher
// null (Go nil) instead of an empty string, so config_repo_id is genuinely
// absent -- distinguishable from an (impossible but defensive) empty-string
// repo ID -- for the operator-facing "why is this applied-only" read path
// this issue requires.
func terraformStateOwningRepoIDValue(ownerRepoID string) any {
	if ownerRepoID == "" {
		return nil
	}
	return ownerRepoID
}
