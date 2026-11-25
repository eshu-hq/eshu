// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
)

// Runtime-context fact kinds read at query time (#5746).
// serviceCatalogCorrelationFactKind and cicdRunCorrelationFactKind are
// family-local copies of the constants in service/catalog_correlations.go
// (root keeps only the alias in service_alias.go) and
// cicd/run_correlations.go: this package must not import root, so the
// values are duplicated here and MUST stay byte-identical to their
// sources. WorkloadIdentityFactKindQuery and
// PlatformMaterializationFactKindQuery are defined here because no other
// query surface reads them yet.
const (
	WorkloadIdentityFactKindQuery         = "reducer_workload_identity"
	PlatformMaterializationFactKindQuery  = "reducer_platform_materialization"
	serviceCatalogCorrelationFactKind     = "reducer_service_catalog_correlation"
	cicdRunCorrelationFactKind            = "reducer_ci_cd_run_correlation"
	RuntimeEnvironmentEvidenceDeployEvent = "deploy_event"
	RuntimeEnvironmentEvidenceDeclared    = "declared"
)

// RuntimeContextFactKinds is the closed kind set the
// read-time runtime-context join scans (issue #5746). Each kind contributes
// one side of the repository→runtime mapping: workload_identity owns
// workloads, service_catalog_correlation owns services and catalog refs,
// platform_materialization owns deployment ids, and ci_cd_run_correlation
// owns environments — the same four sources the reducer matches at reduce
// time (matchingSupplyChainWorkloads/Services/DeploymentLanes/Deployments).
var RuntimeContextFactKinds = []string{
	WorkloadIdentityFactKindQuery,
	serviceCatalogCorrelationFactKind,
	PlatformMaterializationFactKindQuery,
	cicdRunCorrelationFactKind,
}

// SelectRuntimeContextQuery loads active runtime-context
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
// The query is UNION ALL of one scope-closure arm over all four kinds (the
// C1-proven Arm1 shape: envelope scope in the closure) plus one bounded
// outside-closure arm per kind (envelope outside the closure over that
// kind's partial anchor). The outside arms carry the contract-carried cells
// the closure cannot see — payload repository_id/repo_id, payload-scope
// alias, related-only, and raw-scope rows — with no new index: C1b census
// (2026-11-25: wi 1345, pm 1331, svc 2, ci 20 active rows) bounds each scan,
// and per-kind row counts ride the handler span (see the
// eshu.query.runtime_context_rows_* attributes) so census growth is visible
// at 3 AM. Re-proof trigger: any kind growing ~10x past its C1b census, or
// total active scopes growing ~10x past 16.7k (the outside arms loop scopes
// through the kind anchors), re-opens the per-arm ≤5s headroom proof before
// deploy pressure lands.
//
// Bounded by len(candidates) (page-sized, at most the enforced findings page
// limit of supplyChainImpactFindingMaxLimit = 200) and 4 kinds — this is the
// exact join shape #5747's filter rework reuses, so it MUST hold the
// performance contract at corpus scale (proven with EXPLAIN ANALYZE per arm
// plus the combined statement on the worst-case 200-candidate partition).
var SelectRuntimeContextQuery = buildSelectSupplyChainImpactRuntimeContextQuery()

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
func (s PostgresFindingStore) ListSupplyChainImpactRuntimeContext(
	ctx context.Context,
	repositoryIDs []string,
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
) (map[string]RuntimeContext, error) {
	out := make(map[string]RuntimeContext)
	if len(repositoryIDs) == 0 {
		return out, nil
	}
	if s.DB == nil && s.reader == nil {
		// Fail loud like the sibling list read (ListSupplyChainImpactFindings):
		// an unconfigured store returning honest-empty contexts on every finding would
		// be indistinguishable from "nothing runs this" to a caller.
		return nil, fmt.Errorf("supply chain impact runtime context database is required")
	}
	rows, err := s.queryer().QueryContext(
		ctx,
		SelectRuntimeContextQuery,
		array.Of(repositoryIDs),
		array.Of(allowedRepositoryIDs),
		array.Of(allowedScopeIDs),
		array.Of(supplyChainRuntimeContextClosureScopes(repositoryIDs)),
	)
	if err != nil {
		return nil, fmt.Errorf("list supply chain impact runtime context: %w", err)
	}
	defer func() { _ = rows.Close() }()
	// Per-kind scanned-row counts feed the census-growth watchpoint (#11447
	// Plan C re-proof trigger): C1b baselines are wi 1345, pm 1331, svc 2,
	// ci 20 active rows on 2026-11-25, with 16.7k total active scopes. The
	// handler span carries them so growth toward ~10x is visible at 3 AM
	// next to the runtime_context stage duration.
	var workloadIdentityRows, serviceCatalogRows, platformMaterializationRows, cicdRunRows int
	for rows.Next() {
		var kind, scopeID, repositoryID string
		var payloadBytes []byte
		if err := rows.Scan(&kind, &scopeID, &payloadBytes, &repositoryID); err != nil {
			return nil, fmt.Errorf("scan supply chain impact runtime context: %w", err)
		}
		switch kind {
		case WorkloadIdentityFactKindQuery:
			workloadIdentityRows++
		case serviceCatalogCorrelationFactKind:
			serviceCatalogRows++
		case PlatformMaterializationFactKindQuery:
			platformMaterializationRows++
		case cicdRunCorrelationFactKind:
			cicdRunRows++
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
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.Int("eshu.query.runtime_context_rows_workload_identity", workloadIdentityRows),
		attribute.Int("eshu.query.runtime_context_rows_service_catalog", serviceCatalogRows),
		attribute.Int("eshu.query.runtime_context_rows_platform_materialization", platformMaterializationRows),
		attribute.Int("eshu.query.runtime_context_rows_ci_cd_run", cicdRunRows),
	)
	return out, nil
}

// AddRuntimeContextFact folds one runtime-context fact into the
// per-repository result map. The fact's repository anchor is decoded with the
// same precedence the reducer uses (payload repository_id/repo_id first, then
// a repository:-prefixed scope); a fact that decodes to no repository is
// ignored because it can never join a finding.
func AddRuntimeContextFact(
	out map[string]RuntimeContext,
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
	out map[string]RuntimeContext,
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

// RecordRuntimeEnvironmentEvidence folds one exact-digest lookup
// row into the response-side evidence map. It mirrors the reducer's #5426
// contract: only deploy_event proves deployment-event corroboration, every
// missing or unknown admitted value is declared, and deploy_event wins
// independent of fact iteration order.
func RecordRuntimeEnvironmentEvidence(
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
	if state[environment] == RuntimeEnvironmentEvidenceDeployEvent {
		return state
	}
	if strings.TrimSpace(raw) == RuntimeEnvironmentEvidenceDeployEvent {
		state[environment] = RuntimeEnvironmentEvidenceDeployEvent
		return state
	}
	state[environment] = RuntimeEnvironmentEvidenceDeclared
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
	if rest, ok := strings.CutPrefix(scopeID, scope.GitRepositoryScopePrefix); ok {
		if trimmed := strings.TrimSpace(rest); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
