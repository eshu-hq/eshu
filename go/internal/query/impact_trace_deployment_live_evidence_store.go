// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/lib/pq"

	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

// argoCDTrackingIDAnnotationKey is the ArgoCD annotation key
// (argocd.argoproj.io/tracking-id) the kubernetes_live.pod_template
// collector allowlists onto payload.annotations (#5471 F2,
// go/internal/collector/kuberneteslive/clientgo/client.go
// identityAnnotationAllowlist). It is the declared->live identity binding
// key PostgresKubernetesPodTemplateStore anchors on.
const argoCDTrackingIDAnnotationKey = "argocd.argoproj.io/tracking-id"

// kubernetesPodTemplateFactKind is the reducer-facing fact kind for one live
// pod-template-backed Kubernetes workload observation
// (kuberneteslivev1.PodTemplate, sdk/go/factschema).
const kubernetesPodTemplateFactKind = factschema.FactKindKubernetesLivePodTemplate

// KubernetesPodTemplateStore reads active kubernetes_live.pod_template facts
// anchored on the ArgoCD tracking-id identity annotation (#5471 codex P1
// fix). It is the read half of the declared->live identity binding
// fetchWorkloadLiveEvidence uses to promote a workload's deployment truth
// tier: an identity-bound live pod matching the traced workload's own
// ArgoCD Application/group/kind/namespace/name is required before an
// image-digest match can promote to runtime_confirmed, closing the
// cross-workload false-positive a shared image digest alone previously
// allowed (fetchWorkloadLiveEvidence used to match on image_ref alone).
type KubernetesPodTemplateStore interface {
	// HasLiveIdentityMatch reports whether an ACTIVE kubernetes_live.pod_template
	// fact exists whose argocd.argoproj.io/tracking-id annotation equals
	// filter.TrackingID (and, when filter.ImageRefs is non-empty, whose
	// declared image_refs intersect it).
	HasLiveIdentityMatch(context.Context, KubernetesPodTemplateFilter) (bool, error)
}

// KubernetesPodTemplateFilter bounds a live pod-template identity read to a
// concrete ArgoCD tracking-id, optionally narrowed to a set of declared
// image references. TrackingID is required so a read never scans the whole
// fact store on an unanchored predicate.
type KubernetesPodTemplateFilter struct {
	// TrackingID is the expected argocd.argoproj.io/tracking-id value
	// (expectedArgoCDTrackingIDs) the caller is probing for. Required.
	TrackingID string
	// ImageRefs narrows the match to a live pod template whose
	// payload.image_refs array contains at least one of these declared
	// image references (digest confirmation on top of identity). The
	// current caller, fetchWorkloadLiveEvidence
	// (impact_trace_deployment_live_evidence.go), never queries the store
	// with an empty ImageRefs -- it returns false before querying when
	// imageRefs is empty -- so every live match today pairs the tracking-id
	// identity with a non-empty digest confirmation; an identity-only match
	// is not a supported mode of the current caller.
	ImageRefs []string
	// AllScopes, AllowedRepositoryIDs, and AllowedScopeIDs carry the #5167
	// access-scoping bound, identical in shape and intent to
	// KubernetesCorrelationFilter
	// (go/internal/query/kubernetes_correlations.go) -- kubernetes_live
	// facts are scoped the same way (fact.scope_id against the caller's
	// granted repositories/ingestion scopes).
	AllScopes            bool
	AllowedRepositoryIDs []string
	AllowedScopeIDs      []string
}

func (f KubernetesPodTemplateFilter) hasScope() bool {
	return f.TrackingID != ""
}

type kubernetesPodTemplateQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// PostgresKubernetesPodTemplateStore reads active kubernetes_live.pod_template
// facts from Postgres, anchored on the ArgoCD tracking-id identity
// annotation.
type PostgresKubernetesPodTemplateStore struct {
	DB kubernetesPodTemplateQueryer
}

// NewPostgresKubernetesPodTemplateStore creates the Postgres-backed
// kubernetes_live.pod_template identity read model.
func NewPostgresKubernetesPodTemplateStore(db kubernetesPodTemplateQueryer) PostgresKubernetesPodTemplateStore {
	return PostgresKubernetesPodTemplateStore{DB: db}
}

// HasLiveIdentityMatch reports whether an ACTIVE kubernetes_live.pod_template
// fact exists whose argocd.argoproj.io/tracking-id annotation equals
// filter.TrackingID (and, when filter.ImageRefs is non-empty, whose
// payload.image_refs array intersects it). Bounded to LIMIT 1 (existence
// check only); nil-safe (DB == nil returns an error --
// fetchWorkloadLiveEvidence fails closed to false on any error, mirroring
// PostgresKubernetesCorrelationStore's convention).
func (s PostgresKubernetesPodTemplateStore) HasLiveIdentityMatch(
	ctx context.Context,
	filter KubernetesPodTemplateFilter,
) (bool, error) {
	if s.DB == nil {
		return false, fmt.Errorf("kubernetes pod template database is required")
	}
	if !filter.hasScope() {
		return false, fmt.Errorf("tracking_id is required")
	}
	// Defense in depth (#5167, mirrors PostgresKubernetesCorrelationStore):
	// a scoped caller with no granted repository or ingestion scope gets a
	// false match without a query.
	if !filter.AllScopes && len(filter.AllowedRepositoryIDs) == 0 && len(filter.AllowedScopeIDs) == 0 {
		return false, nil
	}

	query := hasLiveKubernetesPodTemplateIdentityQuery
	args := []any{
		kubernetesPodTemplateFactKind,
		argoCDTrackingIDAnnotationKey,
		filter.TrackingID,
		len(filter.ImageRefs) == 0,
		pq.Array(filter.ImageRefs),
	}
	if !filter.AllScopes {
		query = hasLiveKubernetesPodTemplateIdentityScopedQuery
		args = append(args, pq.Array(filter.AllowedRepositoryIDs), pq.Array(filter.AllowedScopeIDs))
	}
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("has live kubernetes pod template identity match: %w", err)
	}
	defer func() { _ = rows.Close() }()

	matched := rows.Next()
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("has live kubernetes pod template identity match: %w", err)
	}
	return matched, nil
}

// hasLiveKubernetesPodTemplateIdentityQuery reuses the exact ACTIVE-generation
// join and is_tombstone predicate from listKubernetesCorrelationsQuery
// (go/internal/query/kubernetes_correlations.go:168-191): fact_records joined
// to ingestion_scopes on the scope's active_generation_id, and to
// scope_generations filtered to status = 'active'. $2/$3 anchor the
// annotation-keyed identity predicate; $4/$5 apply the optional image-refs
// intersection via the jsonb ?| (any-of) operator, which is a no-op (always
// true) when the caller passed no ImageRefs.
const hasLiveKubernetesPodTemplateIdentityQuery = `
SELECT 1
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = $1
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->'annotations'->>$2 = $3
  AND ($4 OR fact.payload->'image_refs' ?| $5)
LIMIT 1
`

// hasLiveKubernetesPodTemplateIdentityScopedQuery is
// hasLiveKubernetesPodTemplateIdentityQuery with the additional #5167
// access-scoping predicate, mirroring
// listKubernetesCorrelationsScopedQuery. Bound only when
// filter.AllScopes is false.
const hasLiveKubernetesPodTemplateIdentityScopedQuery = `
SELECT 1
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = $1
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND fact.payload->'annotations'->>$2 = $3
  AND ($4 OR fact.payload->'image_refs' ?| $5)
  AND (fact.scope_id = ANY($6) OR fact.scope_id = ANY($7))
LIMIT 1
`
