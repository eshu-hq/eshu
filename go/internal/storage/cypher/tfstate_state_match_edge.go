// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"log/slog"

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

// TerraformStateConfigMatchQuery identifies one #5443 MATCHES_STATE
// candidate lookup: the (repo_id, name) pair a TerraformStateResource is
// trying to match against config-declared TerraformResource nodes.
type TerraformStateConfigMatchQuery struct {
	// UID is the requesting TerraformStateResource's uid, echoed back as the
	// key of TerraformStateConfigMatchResolver's result map.
	UID string
	// OwningRepoID is the resolved config repo (row.OwningRepoID) to search
	// within.
	OwningRepoID string
	// Address is the config-declared bare address (row.Address) to match by
	// exact equality, mirroring canonicalTerraformStateMatchesConfigEdgeCypher's
	// own anchor.
	Address string
}

// TerraformStateConfigMatchResolver counts, for a batch of (repo_id, name)
// candidate lookups, how many TerraformResource config nodes match each pair
// (#5443 P1 review finding). No uniqueness constraint backs (repo_id, name)
// -- tf_resource_unique is (name, path, line_number) -- so two Terraform
// roots in one monorepo can both declare the same address (e.g. two
// "aws_instance.web" resources under different environments), and the MERGE
// in canonicalTerraformStateMatchesConfigEdgeCypher would otherwise fan an
// edge out to every match. Defined here rather than importing a graph driver
// directly so this package depends on a narrow port, not a Bolt client;
// production wiring (cmd/projector) adapts a read session to this interface,
// running the count as a genuinely separate single-clause query (never a
// WITH/aggregate/WHERE chain fused into the same statement as the write --
// see docs/public/reference/nornicdb-query-pitfalls.md's "Multi-Clause Read
// Queries Silently Corrupt The Projection": that exact chained shape was
// probed against the pinned NornicDB backend while building this fix and
// silently dropped every row).
type TerraformStateConfigMatchResolver interface {
	// CountConfigMatchCandidates returns a map keyed by each query's UID to
	// the number of TerraformResource nodes matching its (OwningRepoID,
	// Address) pair. A UID absent from the result map on a nil error means
	// zero candidates matched (absent, never ambiguous). An error means the
	// whole batch could not be resolved this cycle; callers MUST treat every
	// queried row as ambiguous (fail closed) rather than risk writing a
	// wrong edge.
	CountConfigMatchCandidates(ctx context.Context, queries []TerraformStateConfigMatchQuery) (map[string]int, error)
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

// resolveTerraformStateConfigMatchAmbiguity flags rows whose (OwningRepoID,
// Address) MATCHES_STATE candidate lookup matches more than one
// TerraformResource node, so terraformStateMatchesConfigEdgeStatements can
// exclude them (#5443 P1 review finding). Runs as ONE batched resolver call
// per materialization cycle covering every candidate row, not one call per
// row -- the count is a graph-wide fact independent of any single state
// resource, so batching avoids an N-query round trip. A nil resolver (the
// default; see WithTerraformStateConfigMatchResolver) or an empty candidate
// set (no row has both OwningRepoID and Address) leaves every row unchanged,
// matching resolveTerraformStateOwnership's own "not wired yet" precedent.
// A resolver error fails closed: every queried row is flagged ambiguous
// rather than risk writing an edge this cycle could not actually verify is
// unambiguous.
func (w *CanonicalNodeWriter) resolveTerraformStateConfigMatchAmbiguity(
	ctx context.Context,
	rows []projector.TerraformStateResourceRow,
) []projector.TerraformStateResourceRow {
	if w.tfStateConfigMatchResolver == nil || len(rows) == 0 {
		return rows
	}

	queries := make([]TerraformStateConfigMatchQuery, 0, len(rows))
	for _, row := range rows {
		if row.OwningRepoID == "" || row.Address == "" {
			continue
		}
		queries = append(queries, TerraformStateConfigMatchQuery{
			UID:          row.UID,
			OwningRepoID: row.OwningRepoID,
			Address:      row.Address,
		})
	}
	if len(queries) == 0 {
		return rows
	}

	out := make([]projector.TerraformStateResourceRow, len(rows))
	copy(out, rows)

	counts, err := w.tfStateConfigMatchResolver.CountConfigMatchCandidates(ctx, queries)
	if err != nil {
		slog.WarnContext(
			ctx, "terraform state config-match ambiguity resolution failed; flagging this batch's MATCHES_STATE candidates ambiguous (fail closed)",
			"error", err.Error(),
			"candidate_count", len(queries),
		)
		for i := range out {
			if out[i].OwningRepoID != "" && out[i].Address != "" {
				out[i].ConfigMatchAmbiguous = true
			}
		}
		return out
	}

	for i := range out {
		if out[i].OwningRepoID == "" || out[i].Address == "" {
			continue
		}
		// > 1, not != 1: a count of 0 (no candidate matched) is not
		// ambiguous, just absent -- the edge write's own MATCH already
		// no-ops harmlessly for it, exactly as it did before this fix.
		if counts[out[i].UID] > 1 {
			out[i].ConfigMatchAmbiguous = true
		}
	}
	return out
}

// terraformStateMatchesConfigEdgeStatements builds the batched MATCHES_STATE
// edge write for every row whose ownership resolved (OwningRepoID
// non-empty), whose address is non-blank, and whose config match is NOT
// flagged ambiguous (ConfigMatchAmbiguous; see
// resolveTerraformStateConfigMatchAmbiguity). Rows with no resolved owner
// are silently excluded from this statement -- not an error, since
// "ownership unresolved" is a legitimate, expected state (see
// TerraformStateOwnershipResolver) recorded on the node itself via
// config_repo_id (canonicalTerraformStateResourceUpsertCypher) rather than
// this edge phase. Ambiguous rows are excluded the same way: this
// repository's precedent (see TerraformStateConfigMatchResolver) is to
// record ambiguity honestly and write no edge, never to silently pick one
// candidate.
func (w *CanonicalNodeWriter) terraformStateMatchesConfigEdgeStatements(mat projector.CanonicalMaterialization) []Statement {
	rows := make([]map[string]any, 0, len(mat.TerraformStateResources))
	for _, row := range mat.TerraformStateResources {
		if row.OwningRepoID == "" || row.Address == "" || row.ConfigMatchAmbiguous {
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
