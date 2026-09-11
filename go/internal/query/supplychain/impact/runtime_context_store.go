// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

// Runtime-context fact kinds read at query time (#5746).
// serviceCatalogCorrelationFactKind and cicdRunCorrelationFactKind are
// family-local copies of the root package query constants in
// service_catalog_correlations.go and ci_cd_run_correlations.go: those home
// files stay in root (other query surfaces read them there) and this package
// must not import root, so the values are duplicated here and MUST stay
// byte-identical to their root sources. WorkloadIdentityFactKindQuery and
// PlatformMaterializationFactKindQuery are defined here because no other
// query surface reads them yet.
const (
	WorkloadIdentityFactKindQuery                    = "reducer_workload_identity"
	PlatformMaterializationFactKindQuery             = "reducer_platform_materialization"
	serviceCatalogCorrelationFactKind                = "reducer_service_catalog_correlation"
	cicdRunCorrelationFactKind                       = "reducer_ci_cd_run_correlation"
	SupplyChainRuntimeEnvironmentEvidenceDeployEvent = "deploy_event"
	SupplyChainRuntimeEnvironmentEvidenceDeclared    = "declared"
)

// SupplyChainImpactRuntimeContextFactKinds is the closed kind set the
// read-time runtime-context join scans (issue #5746). Each kind contributes
// one side of the repository→runtime mapping: workload_identity owns
// workloads, service_catalog_correlation owns services and catalog refs,
// platform_materialization owns deployment ids, and ci_cd_run_correlation
// owns environments — the same four sources the reducer matches at reduce
// time (matchingSupplyChainWorkloads/Services/DeploymentLanes/Deployments).
var SupplyChainImpactRuntimeContextFactKinds = []string{
	WorkloadIdentityFactKindQuery,
	serviceCatalogCorrelationFactKind,
	PlatformMaterializationFactKindQuery,
	cicdRunCorrelationFactKind,
}

// SelectSupplyChainImpactRuntimeContextQuery loads active runtime-context
// facts whose canonical repository anchor matches a candidate repository id.
// The shared decoder applies the reducer's precedence: payload repository_id
// or repo_id; one selected scope (payload scope_id, falling back to envelope
// scope); the first repository-like related_scope_ids entry; then the raw
// selected scope. Authorizing only that decoded value prevents a
// lower-precedence granted anchor from admitting a fact that folds under an
// unauthorized repository. The active-generation joins mirror the findings
// list query so retracted or stale-generation facts never resolve current
// context. Scoped callers additionally require either the decoded repository
// or the fact's direct ingestion scope to be granted, matching #5747's
// filter-membership authorization boundary.
//
// Bounded by len(candidates) (page-sized, at most the enforced findings page
// limit of supplyChainImpactFindingMaxLimit = 200) and 4 kinds — this is the
// exact join shape #5747's filter rework reuses, so it MUST hold the
// performance contract at corpus scale (proven with EXPLAIN ANALYZE on the
// worst-case 200-candidate partition).
const selectSupplyChainImpactRuntimeContextQueryTemplate = `
SELECT fact.fact_kind,
       fact.scope_id,
       fact.payload,
       runtime_repository.repository_id
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
%s
WHERE fact.fact_kind = ANY($1::text[])
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
        (
          COALESCE(cardinality($3::text[]), 0) = 0
          AND COALESCE(cardinality($4::text[]), 0) = 0
        )
        OR runtime_repository.repository_id = ANY($3::text[])
        OR fact.scope_id = ANY($4::text[])
      )
  AND runtime_repository.repository_id = ANY($2::text[])`

var SelectSupplyChainImpactRuntimeContextQuery = fmt.Sprintf(
	selectSupplyChainImpactRuntimeContextQueryTemplate,
	supplyChainRuntimeRepositoryDecoderJoin(
		"fact.payload",
		"fact.scope_id",
		"runtime_repository",
	),
)

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
// or superseded fact never surfaces as current truth. When either grant slice
// is non-empty, only facts authorized by that repository-or-scope union are
// folded into the response; both empty retains unrestricted behavior.
func (s PostgresSupplyChainImpactFindingStore) ListSupplyChainImpactRuntimeContext(
	ctx context.Context,
	repositoryIDs []string,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
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
		SelectSupplyChainImpactRuntimeContextQuery,
		pgarray.Array(SupplyChainImpactRuntimeContextFactKinds),
		pgarray.Array(repositoryIDs),
		pgarray.Array(allowedRepositoryIDs),
		pgarray.Array(allowedScopeIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("list supply chain impact runtime context: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var kind, scopeID, repositoryID string
		var payloadBytes []byte
		if err := rows.Scan(&kind, &scopeID, &payloadBytes, &repositoryID); err != nil {
			return nil, fmt.Errorf("scan supply chain impact runtime context: %w", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(payloadBytes, &payload); err != nil {
			return nil, fmt.Errorf("decode supply chain impact runtime context payload: %w", err)
		}
		addSupplyChainRuntimeContextFactForRepository(out, kind, repositoryID, payload)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read supply chain impact runtime context: %w", err)
	}
	return out, nil
}

// AddSupplyChainRuntimeContextFact folds one runtime-context fact into the
// per-repository result map. The fact's repository anchor is decoded with the
// same precedence the reducer uses (payload repository_id/repo_id first, then
// a repository:-prefixed scope); a fact that decodes to no repository is
// ignored because it can never join a finding.
func AddSupplyChainRuntimeContextFact(
	out map[string]SupplyChainRuntimeContext,
	kind string,
	scopeID string,
	payload map[string]any,
) {
	addSupplyChainRuntimeContextFactForRepository(
		out,
		kind,
		supplyChainRuntimeContextRepositoryID(payload, scopeID),
		payload,
	)
}

func addSupplyChainRuntimeContextFactForRepository(
	out map[string]SupplyChainRuntimeContext,
	kind string,
	repositoryID string,
	payload map[string]any,
) {
	if repositoryID == "" {
		return
	}
	switch kind {
	case WorkloadIdentityFactKindQuery:
		ctx := out[repositoryID]
		// Mirror the reducer's valid workload-id extraction
		// (supplyChainWorkloadIDsFromPayload): payload workload_id first, then
		// entity_keys filtered to workload:-prefixed keys — a non-workload
		// entity key must never become runtime context (or #5747 filter
		// membership). Malformed non-string direct IDs fail closed.
		if workloadID := supplyChainRuntimeContextString(payload, "workload_id"); workloadID != "" {
			ctx.WorkloadIDs = append(ctx.WorkloadIDs, workloadID)
		}
		for _, key := range supplyChainRuntimeContextOrderedStrings(payload, "entity_keys") {
			if workloadID := strings.TrimSpace(key); workloadID != "" && strings.HasPrefix(workloadID, "workload:") {
				ctx.WorkloadIDs = append(ctx.WorkloadIDs, workloadID)
			}
		}
		out[repositoryID] = ctx
	case serviceCatalogCorrelationFactKind:
		if !supplyChainRuntimeContextOutcomeAccepted(payload) {
			return
		}
		ctx := out[repositoryID]
		if serviceID := supplyChainRuntimeContextString(payload, "service_id"); serviceID != "" {
			ctx.ServiceIDs = append(ctx.ServiceIDs, serviceID)
		}
		if workloadID := supplyChainRuntimeContextString(payload, "workload_id"); workloadID != "" {
			ctx.WorkloadIDs = append(ctx.WorkloadIDs, workloadID)
		}
		if entityRef := supplyChainRuntimeContextString(payload, "entity_ref"); entityRef != "" {
			ctx.CatalogEntityRefs = append(ctx.CatalogEntityRefs, entityRef)
		}
		if ownerRef := supplyChainRuntimeContextString(payload, "owner_ref"); ownerRef != "" {
			ctx.CatalogOwnerRefs = append(ctx.CatalogOwnerRefs, ownerRef)
		}
		out[repositoryID] = ctx
	case PlatformMaterializationFactKindQuery:
		ctx := out[repositoryID]
		// Mirror the reducer's deployment-id extraction exactly
		// (supplyChainDeploymentIDsFromPayload): singular deployment_id first,
		// then entity_keys filtered to deployment:-prefixed keys. Unfiltered
		// entity_keys can carry repo:, platform:, aws:, tfstate:, cloud:, or
		// raw canonical fact-id strings from replay/fallback intent paths —
		// those must never surface as deployment anchors.
		if deploymentID := supplyChainRuntimeContextString(payload, "deployment_id"); deploymentID != "" {
			ctx.DeploymentIDs = append(ctx.DeploymentIDs, deploymentID)
		}
		for _, key := range supplyChainRuntimeContextOrderedStrings(payload, "entity_keys") {
			if deploymentID := strings.TrimSpace(key); deploymentID != "" && strings.HasPrefix(deploymentID, "deployment:") {
				ctx.DeploymentIDs = append(ctx.DeploymentIDs, deploymentID)
			}
		}
		out[repositoryID] = ctx
	case cicdRunCorrelationFactKind:
		if !supplyChainRuntimeContextOutcomeAccepted(payload) {
			return
		}
		if environment := supplyChainRuntimeContextString(payload, "environment"); environment != "" {
			ctx := out[repositoryID]
			ctx.Environments = append(ctx.Environments, environment)
			out[repositoryID] = ctx
		}
	}
}

// RecordSupplyChainRuntimeEnvironmentEvidence folds one exact-digest lookup
// row into the response-side evidence map. It mirrors the reducer's #5426
// contract: only deploy_event proves deployment-event corroboration, every
// missing or unknown admitted value is declared, and deploy_event wins
// independent of fact iteration order.
func RecordSupplyChainRuntimeEnvironmentEvidence(
	state map[string]string,
	environment string,
	raw string,
) map[string]string {
	environment = strings.TrimSpace(environment)
	if environment == "" {
		return state
	}
	if state == nil {
		state = make(map[string]string)
	}
	if state[environment] == SupplyChainRuntimeEnvironmentEvidenceDeployEvent {
		return state
	}
	if strings.TrimSpace(raw) == SupplyChainRuntimeEnvironmentEvidenceDeployEvent {
		state[environment] = SupplyChainRuntimeEnvironmentEvidenceDeployEvent
		return state
	}
	state[environment] = SupplyChainRuntimeEnvironmentEvidenceDeclared
	return state
}

// supplyChainRuntimeContextOutcomeAccepted mirrors the reducer's outcome gate
// (matchingSupplyChainServices / matchingSupplyChainDeployments): an
// exact/derived/empty outcome resolves context; ambiguous, rejected,
// unresolved, stale, or provenance-only evidence is skipped.
func supplyChainRuntimeContextOutcomeAccepted(payload map[string]any) bool {
	if supplyChainRuntimeContextBool(payload, "provenance_only") {
		return false
	}
	rawOutcome, exists := payload["outcome"]
	if exists && rawOutcome != nil {
		if _, ok := rawOutcome.(string); !ok {
			return false
		}
	}
	switch strings.TrimSpace(querycontract.StringVal(payload, "outcome")) {
	case "", "exact", "derived":
		return true
	default:
		return false
	}
}

// supplyChainRuntimeContextBool mirrors the reducer's
// payloadBoolPointerValue truth semantics: booleans and trimmed,
// case-insensitive string "true" are true; every other shape is false.
func supplyChainRuntimeContextBool(payload map[string]any, key string) bool {
	switch value := payload[key].(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	default:
		return false
	}
}

// supplyChainRuntimeContextRepositoryID resolves a fact's repository anchor
// mirroring the reducer's supplyChainWorkloadRepositoryID precedence for valid
// string anchors. Malformed non-string direct anchors fail closed instead of
// becoming Go-formatted identities. A direct payload repository_id or repo_id
// is accepted verbatim (consumption-derived anchors use non-prefixed forms like
// github.com/org/repo or repo://acme/api). Otherwise it selects exactly one
// scope with payload scope_id taking precedence over the envelope scope,
// decodes that scope when prefixed, scans related_scope_ids for a prefixed
// repository, then returns the raw selected scope.
func supplyChainRuntimeContextRepositoryID(payload map[string]any, scopeID string) string {
	for _, key := range []string{"repository_id", "repo_id"} {
		if value := supplyChainRuntimeContextString(payload, key); value != "" {
			return value
		}
	}
	scoped := supplyChainRuntimeContextString(payload, "scope_id")
	if scoped == "" {
		scoped = strings.TrimSpace(scopeID)
	}
	if repositoryID := repositoryIDFromRuntimeContextScope(scoped); repositoryID != "" {
		return repositoryID
	}
	for _, relatedScopeID := range supplyChainRuntimeContextOrderedStrings(
		payload,
		"related_scope_ids",
	) {
		if repositoryID := repositoryIDFromRuntimeContextScope(relatedScopeID); repositoryID != "" {
			return repositoryID
		}
	}
	return scoped
}

// supplyChainRuntimeContextString returns a trimmed JSON string value.
// Identity-like direct fields reject arrays, objects, numbers, and booleans so
// Go hydration and Postgres filter membership cannot stringify them differently.
func supplyChainRuntimeContextString(payload map[string]any, key string) string {
	value, ok := payload[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

// supplyChainRuntimeContextOrderedStrings mirrors the reducer's
// payloadOrderedStrings normalization for repository precedence. In
// particular, it preserves order while trimming arrays, accepts the scalar
// string shape, and skips blank values.
func supplyChainRuntimeContextOrderedStrings(payload map[string]any, key string) []string {
	raw, ok := payload[key]
	if !ok {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			if value = strings.TrimSpace(value); value != "" {
				out = append(out, value)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			text := strings.TrimSpace(querycontract.StringVal(map[string]any{"value": value}, "value"))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return []string{trimmed}
		}
	}
	return nil
}

// repositoryIDFromRuntimeContextScope decodes one scope into a repository id,
// mirroring the reducer's repositoryIDFromReducerScope: repository:-prefixed
// scopes pass through, git-repository-scope:-prefixed scopes are stripped.
func repositoryIDFromRuntimeContextScope(scopeID string) string {
	if strings.HasPrefix(scopeID, "repository:") {
		return scopeID
	}
	if rest, ok := strings.CutPrefix(scopeID, "git-repository-scope:"); ok {
		if trimmed := strings.TrimSpace(rest); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
