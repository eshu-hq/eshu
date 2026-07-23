// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// K8sSelectCandidate is the narrow, matcher-only projection of a K8sResource
// content entity used by the impact-trace directed SELECTS candidate scan
// (#5363). It carries ONLY the fields k8sSelectMatch needs -- never the wide
// metadata JSONB -- so the repository-wide candidate fetch does not pay the
// top-N heapsort cost of carrying every row's metadata through the ORDER BY
// (measured ~25 ms wide vs ~12.5 ms narrow at the 5001-row cap on a 6000-K8s
// monorepo; see evidence-5363-impact-trace-k8s-fetch.md). A candidate is used
// for MATCHING ONLY and never reaches the wire: a Service that actually
// selector-matches the traced Deployment is re-fetched by ID through the wide
// EntityContent path (ListRepoEntitiesByIDs) before it joins the surfaced pool.
//
// SelectorPresent and PodTemplateLabelsPresent preserve the key-absent vs
// key-present-but-empty tri-state that k8sSelectMatch depends on (see
// k8sSelectMatchInput). They are true only when the JSON value is a string,
// exactly mirroring the Go comma-ok `metadata[key].(string)` used on the
// EntityContent path (k8sSelectMatchInputFromEntity), so a candidate converts
// losslessly to the same k8sSelectMatchInput the entity path would produce.
type K8sSelectCandidate struct {
	EntityID                 string
	EntityName               string
	Kind                     string
	Namespace                string
	Selector                 string
	SelectorPresent          bool
	PodTemplateLabels        string
	PodTemplateLabelsPresent bool
}

// matchInput adapts a K8sSelectCandidate into the shared k8sSelectMatchInput.
// The mapping is 1:1 with k8sSelectMatchInputFromEntity for the same source
// row, so a directed match over candidates produces byte-for-byte the same
// verdict the entity-context path would produce over the equivalent
// EntityContent.
func (c K8sSelectCandidate) matchInput() k8sSelectMatchInput {
	return k8sSelectMatchInput{
		kind:                     c.Kind,
		name:                     c.EntityName,
		namespace:                c.Namespace,
		selector:                 c.Selector,
		selectorPresent:          c.SelectorPresent,
		podTemplateLabels:        c.PodTemplateLabels,
		podTemplateLabelsPresent: c.PodTemplateLabelsPresent,
	}
}

// k8sSelectCandidateFromEntity projects an EntityContent into the narrow
// K8sSelectCandidate using the same comma-ok tri-state and namespace
// normalization (k8sNamespace) as k8sSelectMatchInputFromEntity. It is the
// in-memory equivalent of the ListRepoK8sSelectCandidates SQL projection and
// keeps test doubles that hold EntityContent rows byte-consistent with the
// production narrow fetch.
func k8sSelectCandidateFromEntity(entity EntityContent) K8sSelectCandidate {
	kind, _ := entity.Metadata["kind"].(string)
	selector, selectorPresent := entity.Metadata["selector"].(string)
	podTemplateLabels, podTemplateLabelsPresent := entity.Metadata["pod_template_labels"].(string)
	return K8sSelectCandidate{
		EntityID:                 entity.EntityID,
		EntityName:               entity.EntityName,
		Kind:                     kind,
		Namespace:                k8sNamespace(entity.Metadata),
		Selector:                 selector,
		SelectorPresent:          selectorPresent,
		PodTemplateLabels:        podTemplateLabels,
		PodTemplateLabelsPresent: podTemplateLabelsPresent,
	}
}

// ListRepoK8sSelectCandidates returns the narrow, matcher-only projection of
// every K8sResource in repoID, up to limit rows, ordered deterministically by
// relative_path, start_line, entity_id so a truncated fetch drops a
// reproducible tail (truncation honesty; #5367 keyset pagination reuses the
// same order). It projects only the fields k8sSelectMatch needs and never the
// wide metadata JSONB, so the ORDER BY sorts narrow rows.
//
// The selector/pod_template_labels presence columns use
// jsonb_typeof(metadata->'key') = 'string' rather than the key-exists operator
// so presence is true only for a JSON string value -- byte-for-byte with the
// Go comma-ok `metadata[key].(string)` on the EntityContent path
// (k8sSelectMatchInputFromEntity). A present-but-null or non-string value is
// treated as absent by both paths, preserving the tri-state the matcher's
// anti-false-positive-masking logic depends on. namespace is trimmed in Go to
// mirror k8sNamespace exactly (namespace equality is a correctness gate).
//
// There is intentionally no SQL kind filter: candidacy (kind == "Service") is
// decided in Go by the caller. #5490 measured a SQL-level
// lower(metadata->>'kind') = 'service' pushdown and rejected it: without a
// supporting expression index it forces a whole-table Seq Scan (a
// platform-wide, not per-repo, cost), and it is unnecessary once the
// migration 077 partial index below makes the fetch itself ~1.7-2.0 ms, at
// which point filtering Kind in Go over the resulting <=5001-row slice is
// free. See docs/internal/evidence/5490-k8sresource-candidate-index.md.
//
// This query is served by the partial covering index
// content_entities_k8s_select_partial_idx (migration 077,
// go/internal/storage/postgres/migrations/077_content_entities_k8s_select_partial_index.sql),
// which matches the ORDER BY key and INCLUDEs entity_name/metadata so the
// planner can satisfy this fetch with an Index Only Scan and no Sort node
// (measured ~11-19 ms -> ~1.7-2.0 ms on the #5363/#5490 worst-case
// partition). The index is partial (WHERE entity_type = 'K8sResource') so
// its write-amplification is confined to K8sResource rows and does not tax
// every other entity_type write on this hot ingest table.
func (cr *ContentReader) ListRepoK8sSelectCandidates(ctx context.Context, repoID string, limit int) ([]K8sSelectCandidate, error) {
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_repo_k8s_select_candidates"),
			attribute.String("db.sql.table", "content_entities"),
		),
	)
	defer span.End()

	if limit <= 0 {
		limit = 500
	}

	rows, err := cr.db.QueryContext(ctx, `
		SELECT entity_id,
		       entity_name,
		       coalesce(metadata->>'kind', ''),
		       coalesce(metadata->>'namespace', ''),
		       coalesce(jsonb_typeof(metadata->'selector') = 'string', false),
		       coalesce(metadata->>'selector', ''),
		       coalesce(jsonb_typeof(metadata->'pod_template_labels') = 'string', false),
		       coalesce(metadata->>'pod_template_labels', '')
		FROM content_entities
		WHERE repo_id = $1 AND entity_type = 'K8sResource'
		ORDER BY relative_path, start_line, entity_id
		LIMIT $2
	`, repoID, limit)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("list repo k8s select candidates: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var results []K8sSelectCandidate
	for rows.Next() {
		var candidate K8sSelectCandidate
		var namespace string
		if err := rows.Scan(
			&candidate.EntityID,
			&candidate.EntityName,
			&candidate.Kind,
			&namespace,
			&candidate.SelectorPresent,
			&candidate.Selector,
			&candidate.PodTemplateLabelsPresent,
			&candidate.PodTemplateLabels,
		); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan repo k8s select candidate: %w", err)
		}
		candidate.Namespace = strings.TrimSpace(namespace)
		results = append(results, candidate)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return results, err
	}
	return results, nil
}
