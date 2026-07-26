// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// Runtime-context fact kinds read at query time (#5746). cicdRunCorrelationFactKind
// and serviceCatalogCorrelationFactKind already exist in this package
// (ci_cd_run_correlations.go, service_catalog_correlations.go); the other two
// are defined here because no other query surface reads them yet.
const (
	workloadIdentityFactKindQuery        = "reducer_workload_identity"
	platformMaterializationFactKindQuery = "reducer_platform_materialization"
)

// supplyChainImpactRuntimeContextFactKinds is the closed kind set the
// read-time runtime-context join scans (issue #5746). Each kind contributes
// one side of the repository→runtime mapping: workload_identity owns
// workloads, service_catalog_correlation owns services and catalog refs,
// platform_materialization owns deployment ids, and ci_cd_run_correlation
// owns environments — the same four sources the reducer matches at reduce
// time (matchingSupplyChainWorkloads/Services/DeploymentLanes/Deployments).
var supplyChainImpactRuntimeContextFactKinds = []string{
	workloadIdentityFactKindQuery,
	serviceCatalogCorrelationFactKind,
	platformMaterializationFactKindQuery,
	cicdRunCorrelationFactKind,
}

// selectSupplyChainImpactRuntimeContextQuery loads active runtime-context
// facts whose repository anchor matches any candidate repository id. A fact
// anchors a repository when ANY of these holds: its payload repository_id or
// repo_id equals a candidate (service/platform/cicd facts), its scope_id
// equals a candidate directly (git-source scopes), its scope_id equals a
// git-repository-scope:-prefixed candidate (workload_identity and
// platform_materialization facts, whose scopes carry that prefix — verified
// against the live corpus), or its payload scope_id equals either form. The
// active-generation joins mirror the findings list query so a retracted or
// stale-generation fact never resolves current context.
//
// Bounded by len(candidates) (page-sized, <= ~50) and 4 kinds — this is the
// exact join shape #5747's filter rework reuses, so it MUST hold the
// performance contract at corpus scale (proven with EXPLAIN ANALYZE on the
// worst-case partition).
const selectSupplyChainImpactRuntimeContextQuery = `
SELECT fact.fact_kind,
       fact.scope_id,
       fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = ANY($1::text[])
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
        COALESCE(fact.payload->>'repository_id', fact.payload->>'repo_id', '') = ANY($2::text[])
        OR fact.scope_id = ANY($2::text[])
        OR fact.scope_id IN (SELECT 'git-repository-scope:' || candidate FROM unnest($2::text[]) AS candidate)
        OR COALESCE(fact.payload->>'scope_id', '') = ANY($2::text[])
        OR COALESCE(fact.payload->>'scope_id', '') IN (SELECT 'git-repository-scope:' || candidate FROM unnest($2::text[]) AS candidate)
      )`

// ListSupplyChainImpactRuntimeContext resolves the CURRENT runtime context
// (workloads, services, deployments, environments, catalog refs) for each
// candidate repository id from active workload_identity,
// service_catalog_correlation, platform_materialization, and
// ci_cd_run_correlation facts (issue #5746). Repositories with no matching
// facts are simply absent from the returned map; the caller renders that as
// an honest empty context (fresh ingest self-heals on the next read).
//
// Outcome gates mirror the reducer's reduce-time rules: exact/derived
// correlations (or an empty outcome) resolve context; ambiguous, rejected,
// unresolved, stale, or provenance-only evidence is skipped so a contested
// or superseded fact never surfaces as current truth.
func (s PostgresSupplyChainImpactFindingStore) ListSupplyChainImpactRuntimeContext(
	ctx context.Context,
	repositoryIDs []string,
) (map[string]SupplyChainRuntimeContext, error) {
	out := make(map[string]SupplyChainRuntimeContext)
	if len(repositoryIDs) == 0 {
		return out, nil
	}
	if s.DB == nil {
		// Fail loud like the sibling list read (ListSupplyChainImpactFindings):
		// a nil-DB store returning honest-empty contexts on every finding would
		// be indistinguishable from "nothing runs this" to a caller.
		return nil, fmt.Errorf("supply chain impact runtime context database is required")
	}
	rows, err := s.DB.QueryContext(
		ctx,
		selectSupplyChainImpactRuntimeContextQuery,
		pq.Array(supplyChainImpactRuntimeContextFactKinds),
		pq.Array(repositoryIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("list supply chain impact runtime context: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var kind, scopeID string
		var payloadBytes []byte
		if err := rows.Scan(&kind, &scopeID, &payloadBytes); err != nil {
			return nil, fmt.Errorf("scan supply chain impact runtime context: %w", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil, fmt.Errorf("decode supply chain impact runtime context payload: %w", err)
		}
		addSupplyChainRuntimeContextFact(out, kind, scopeID, payload)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read supply chain impact runtime context: %w", err)
	}
	return out, nil
}

// addSupplyChainRuntimeContextFact folds one runtime-context fact into the
// per-repository result map. The fact's repository anchor is decoded with the
// same precedence the reducer uses (payload repository_id/repo_id first, then
// a repository:-prefixed scope); a fact that decodes to no repository is
// ignored because it can never join a finding.
func addSupplyChainRuntimeContextFact(
	out map[string]SupplyChainRuntimeContext,
	kind string,
	scopeID string,
	payload map[string]any,
) {
	switch kind {
	case workloadIdentityFactKindQuery:
		repositoryID := supplyChainRuntimeContextRepositoryID(payload, scopeID)
		if repositoryID == "" {
			return
		}
		ctx := out[repositoryID]
		// Mirror the reducer's workload-id extraction exactly
		// (supplyChainWorkloadIDsFromPayload): payload workload_id first, then
		// entity_keys filtered to workload:-prefixed keys — a non-workload
		// entity key must never become runtime context (or #5747 filter
		// membership).
		if workloadID := strings.TrimSpace(StringVal(payload, "workload_id")); workloadID != "" {
			ctx.WorkloadIDs = append(ctx.WorkloadIDs, workloadID)
		}
		for _, key := range StringSliceVal(payload, "entity_keys") {
			if workloadID := strings.TrimSpace(key); workloadID != "" && strings.HasPrefix(workloadID, "workload:") {
				ctx.WorkloadIDs = append(ctx.WorkloadIDs, workloadID)
			}
		}
		out[repositoryID] = ctx
	case serviceCatalogCorrelationFactKind:
		if !supplyChainRuntimeContextOutcomeAccepted(payload) {
			return
		}
		repositoryID := supplyChainRuntimeContextRepositoryID(payload, scopeID)
		if repositoryID == "" {
			return
		}
		ctx := out[repositoryID]
		if serviceID := strings.TrimSpace(StringVal(payload, "service_id")); serviceID != "" {
			ctx.ServiceIDs = append(ctx.ServiceIDs, serviceID)
		}
		if workloadID := strings.TrimSpace(StringVal(payload, "workload_id")); workloadID != "" {
			ctx.WorkloadIDs = append(ctx.WorkloadIDs, workloadID)
		}
		if entityRef := strings.TrimSpace(StringVal(payload, "entity_ref")); entityRef != "" {
			ctx.CatalogEntityRefs = append(ctx.CatalogEntityRefs, entityRef)
		}
		if ownerRef := strings.TrimSpace(StringVal(payload, "owner_ref")); ownerRef != "" {
			ctx.CatalogOwnerRefs = append(ctx.CatalogOwnerRefs, ownerRef)
		}
		out[repositoryID] = ctx
	case platformMaterializationFactKindQuery:
		repositoryID := supplyChainRuntimeContextRepositoryID(payload, scopeID)
		if repositoryID == "" {
			return
		}
		ctx := out[repositoryID]
		// The platform-materialization writer records deployment ids under
		// entity_keys (verified against the live corpus payload); deployment_ids
		// is the older key retained as a fallback for other writers.
		deploymentIDs := StringSliceVal(payload, "entity_keys")
		if len(deploymentIDs) == 0 {
			deploymentIDs = StringSliceVal(payload, "deployment_ids")
		}
		for _, id := range deploymentIDs {
			if deploymentID := strings.TrimSpace(id); deploymentID != "" {
				ctx.DeploymentIDs = append(ctx.DeploymentIDs, deploymentID)
			}
		}
		out[repositoryID] = ctx
	case cicdRunCorrelationFactKind:
		if !supplyChainRuntimeContextOutcomeAccepted(payload) {
			return
		}
		repositoryID := supplyChainRuntimeContextRepositoryID(payload, scopeID)
		if repositoryID == "" {
			return
		}
		if environment := strings.TrimSpace(StringVal(payload, "environment")); environment != "" {
			ctx := out[repositoryID]
			ctx.Environments = append(ctx.Environments, environment)
			out[repositoryID] = ctx
		}
	}
}

// supplyChainRuntimeContextOutcomeAccepted mirrors the reducer's outcome gate
// (matchingSupplyChainServices / matchingSupplyChainDeployments): an
// exact/derived/empty outcome resolves context; ambiguous, rejected,
// unresolved, stale, or provenance-only evidence is skipped.
func supplyChainRuntimeContextOutcomeAccepted(payload map[string]any) bool {
	if BoolVal(payload, "provenance_only") {
		return false
	}
	switch strings.TrimSpace(StringVal(payload, "outcome")) {
	case "", "exact", "derived":
		return true
	default:
		return false
	}
}

// supplyChainRuntimeContextRepositoryID resolves a fact's repository anchor
// with the reducer's precedence: payload repository_id, then repo_id, then a
// repository:-prefixed scope_id (payload or envelope). OCI-registry paths and
// opaque scan scopes decode to "" — they can never join a finding's git
// repository id, matching the #5464 anchor discipline.
func supplyChainRuntimeContextRepositoryID(payload map[string]any, scopeID string) string {
	for _, key := range []string{"repository_id", "repo_id"} {
		if value := strings.TrimSpace(StringVal(payload, key)); value != "" && strings.HasPrefix(value, "repository:") {
			return value
		}
	}
	for _, candidate := range []string{strings.TrimSpace(StringVal(payload, "scope_id")), strings.TrimSpace(scopeID)} {
		if strings.HasPrefix(candidate, "repository:") {
			return candidate
		}
		if rest, ok := strings.CutPrefix(candidate, "git-repository-scope:"); ok {
			if trimmed := strings.TrimSpace(rest); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}
