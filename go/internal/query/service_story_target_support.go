// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B4 EntityHandler/ContentReader seam stayer for #6060: methods on those types must stay in package query, so this file cannot move to service/

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const serviceStoryTargetSupportLimit = querycontract.ServiceStoryTargetSupportLimit

// The row read links a support fact to a target through the one durable key a
// support writer emits: work_item.external_link payload->>'linked_repository_id'
// (collector/jira, a confidently typed GitHub pull-request or GitLab
// merge-request link resolved to the canonical repository id). No other support
// fact kind carries a target key today; record, transition and metadata rows and
// the PagerDuty kinds stay source-only until #7463 and #7464 link them. The
// migration 151 partial index and TestServiceStoryTargetSupportLinkIndexMatchesQuery
// bind these literals to the statement.
const (
	storySupportLinkFactKind      = "work_item.external_link"
	storySupportLinkPayloadKey    = "linked_repository_id"
	storySupportLinkKeyExpression = "payload->>'" + storySupportLinkPayloadKey + "'"

	// Per-row link basis values stamped on evidence and ambiguous rows.
	storySupportBasisLinkedRepository = "linked_repository"
	storySupportBasisSoleWorkload     = "repository_sole_workload"
	storySupportBasisSharedRepository = "repository_multiple_workloads"
)

type serviceStoryTargetSupportStore interface {
	ServiceStoryTargetSupportEvidence(
		context.Context,
		serviceStoryTargetSupportFilter,
	) (serviceStoryTargetSupportReadModel, error)
}

// The target-support filter and read model are aliases onto querycontract, so
// this package's call sites keep their unexported spelling while a
// ContentStore double outside package query can still name them (#6060).
type (
	serviceStoryTargetSupportFilter    = querycontract.ServiceStoryTargetSupportFilter
	serviceStoryTargetSupportReadModel = querycontract.ServiceStoryTargetSupportReadModel
)

// ServiceStoryTargetSupportEvidence reads support-evidence rows for
// filter.TargetKind/filter.TargetID (service or repository) from
// fact_records via Postgres, grouped into the read model's Support map. It
// is the read-model path serviceStoryTargetSupportStore exposes to
// loadServiceStoryTargetSupport and loadRepositoryStoryTargetSupport. This
// is the #6060 audit's worst fallback case: without a satisfying store,
// those callers return (nil, nil) with no fallback of any kind, so the
// target_support/support_overview response section silently vanishes
// rather than erroring or degrading.
func (cr *ContentReader) ServiceStoryTargetSupportEvidence(
	ctx context.Context,
	filter serviceStoryTargetSupportFilter,
) (serviceStoryTargetSupportReadModel, error) {
	if cr == nil || cr.db == nil {
		return serviceStoryTargetSupportReadModel{}, nil
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_service_story_target_support"),
			attribute.String("db.sql.table", "fact_records"),
		),
	)
	defer span.End()

	factKinds := serviceStoryTargetSupportFactKinds()
	scope := querycontract.DocumentationTargetScopeFromValues(
		filter.Repository,
		filter.TargetKind,
		filter.TargetID,
		filter.ServiceID,
	)
	hasSelector := querycontract.DocumentationTargetScopeHasSelector(scope)
	limit := serviceStoryTargetSupportRowLimit(filter.Limit)
	facts := make([]map[string]any, 0, limit)
	// An empty statement means no target-linked row can exist: the target has no
	// repository id, or a service target whose repository the graph did not show
	// defining exactly-or-among the target. The read then takes the zero-row
	// path below instead of asking Postgres for rows it would discard (#7138).
	query, args := buildServiceStoryTargetSupportSQL(filter)
	if query == "" && !hasSelector {
		return serviceStoryTargetSupportReadModel{}, nil
	}
	if query != "" {
		rows, err := cr.db.QueryContext(ctx, query, args...)
		if err != nil {
			span.RecordError(err)
			return serviceStoryTargetSupportReadModel{}, fmt.Errorf("query service story target support: %w", err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			payload, err := scanJSONPayload(rows)
			if err != nil {
				span.RecordError(err)
				return serviceStoryTargetSupportReadModel{}, fmt.Errorf("query service story target support: %w", err)
			}
			facts = append(facts, payload)
		}
		if err := rows.Err(); err != nil {
			span.RecordError(err)
			return serviceStoryTargetSupportReadModel{}, fmt.Errorf("query service story target support: %w", err)
		}
	}
	truncated := len(facts) > limit
	if truncated {
		facts = facts[:limit]
	}
	var sourceOnlySummary serviceStoryTargetSupportSourceOnlySummary
	if len(facts) == 0 && hasSelector {
		var err error
		sourceOnlySummary, err = cr.serviceStoryTargetSupportSourceOnlySummary(ctx, factKinds)
		if err != nil {
			span.RecordError(err)
			return serviceStoryTargetSupportReadModel{}, err
		}
	}
	return serviceStoryTargetSupportReadModel{
		Support: buildStoryTargetSupportWithSourceOnlySummary(filter, facts, truncated, sourceOnlySummary),
	}, nil
}

// storySupportLinkRepositoryID returns the repository id the row read matches
// against payload->>'linked_repository_id', or "" when no target-linked row can
// exist and the read must not run.
//
// A repository target matches its own id. A service target matches the id of
// the repository that hosts it, but only when the graph showed that repository
// defining the target: the service then owns the repository's links outright
// (one defined workload) or shares them (several, reported ambiguous). Any
// other service case, including a graph that was unavailable, is fail closed.
// A target kind the story does not serve has no link key at all.
func storySupportLinkRepositoryID(filter serviceStoryTargetSupportFilter) string {
	switch strings.TrimSpace(filter.TargetKind) {
	case "repository":
		if id := strings.TrimSpace(filter.TargetID); id != "" {
			return id
		}
		return strings.TrimSpace(filter.Repository)
	case "service":
		if !filter.RepositoryDefinesTarget || filter.RepositoryWorkloadCount < 1 {
			return ""
		}
		return strings.TrimSpace(filter.Repository)
	}
	return ""
}

// buildServiceStoryTargetSupportSQL renders the bounded row read: the active
// work_item.external_link facts whose linked_repository_id is the target's
// repository. The predicate is the plain payload->>'key' = $1 form on purpose.
// Migration 151's partial expression index is keyed on exactly that expression,
// and a NULLIF wrapper would make it unusable (measured in the #7138 shim: the
// index turns a 13 ms heap filter at 50k links per scope into a 1.7 ms descent).
// The kind, tombstone and key conditions are literal or bound inside the LATERAL
// so Postgres proves the index predicate in custom and generic plans alike. No
// name, title, summary or LIKE predicate is ever added.
func buildServiceStoryTargetSupportSQL(filter serviceStoryTargetSupportFilter) (string, []any) {
	repoID := storySupportLinkRepositoryID(filter)
	if repoID == "" {
		return "", nil
	}
	limit := serviceStoryTargetSupportRowLimit(filter.Limit)
	args := []any{repoID, limit + 1}
	return `
SELECT jsonb_build_object(
    'fact_id', fact.fact_id,
    'fact_kind', fact.fact_kind,
    'scope_id', fact.scope_id,
    'generation_id', fact.generation_id,
    'source_system', fact.source_system,
    'source_record_id', fact.source_record_id,
    'observed_at', fact.observed_at,
    'payload', fact.payload
) AS payload
FROM ingestion_scopes AS scope
JOIN scope_generations AS generation
  ON generation.scope_id = scope.scope_id
 AND generation.generation_id = scope.active_generation_id
CROSS JOIN LATERAL (
  SELECT ` + serviceStoryTargetSupportFactColumns + `
  FROM fact_records AS fact
  WHERE fact.scope_id = scope.scope_id
    AND fact.generation_id = scope.active_generation_id
    AND fact.fact_kind = '` + storySupportLinkFactKind + `'
    AND fact.is_tombstone = FALSE
    AND fact.` + storySupportLinkKeyExpression + ` = $1
  OFFSET 0
) AS fact
WHERE generation.status = 'active'
ORDER BY fact.observed_at DESC, fact.fact_id DESC
LIMIT $2
`, args
}

// serviceStoryTargetSupportFactColumns are the fact columns the target-support
// row read projects.
const serviceStoryTargetSupportFactColumns = `fact.fact_id,
         fact.fact_kind,
         fact.scope_id,
         fact.generation_id,
         fact.source_system,
         fact.source_record_id,
         fact.observed_at,
         fact.payload`

// serviceStoryTargetSupportActiveFactsFrom returns the FROM/WHERE clause the
// source-only aggregate uses (the row read has its own single-kind probe, see
// buildServiceStoryTargetSupportSQL): facts of the $1::text[] kinds on each
// scope's active generation, restricted by factPredicates (fact.* conditions
// ANDed inside the per-probe subquery) and exposing factColumns as fact.* to
// the outer query.
// Each (active scope/generation, kind) pair is probed through a LATERAL
// subquery so fact_records_scope_generation_idx
// (scope_id, generation_id, fact_kind, ...) answers with the kind in the index
// condition (#6794). A plain join made Postgres estimate one fact per active
// pair (thousands in reality; extended statistics do not apply to join
// clauses) and scan every fact of every active generation through the keyset
// index, filtering kinds in the heap. OFFSET 0 is load-bearing: it stops the
// planner from flattening the LATERAL back into that join. DISTINCT keeps a
// repeated kind from counting its facts twice, matching fact_kind = ANY($1).
func serviceStoryTargetSupportActiveFactsFrom(factColumns string, factPredicates []string) string {
	return `FROM ingestion_scopes AS scope
JOIN scope_generations AS generation
  ON generation.scope_id = scope.scope_id
 AND generation.generation_id = scope.active_generation_id
CROSS JOIN (SELECT DISTINCT unnest($1::text[]) AS fact_kind) AS kind
CROSS JOIN LATERAL (
  SELECT ` + factColumns + `
  FROM fact_records AS fact
  WHERE fact.scope_id = scope.scope_id
    AND fact.generation_id = scope.active_generation_id
    AND fact.fact_kind = kind.fact_kind
    AND fact.fact_kind IN (` + serviceStoryTargetSupportKindLiterals() + `)
    AND ` + strings.Join(factPredicates, "\n    AND ") + `
  OFFSET 0
) AS fact
WHERE generation.status = 'active'`
}

// serviceStoryTargetSupportKindLiterals renders the support fact kinds as a SQL
// literal list for the LATERAL probe. It is redundant with the bound `$1` kind
// array, which still drives the per-kind probes, but a literal list is what the
// planner can prove implies migration 123's partial index predicate in a custom
// or generic plan; `fact_kind = kind.fact_kind` alone is not provable. The kinds
// are compile-time constants, never caller input. A caller that binds a subset
// of kinds still counts only that subset, since both conditions must hold.
func serviceStoryTargetSupportKindLiterals() string {
	kinds := serviceStoryTargetSupportFactKinds()
	quoted := make([]string, len(kinds))
	for i, kind := range kinds {
		quoted[i] = "'" + kind + "'"
	}
	return strings.Join(quoted, ", ")
}

func serviceStoryTargetSupportFactKinds() []string {
	kinds := append([]string{}, workItemEvidenceFactKinds...)
	kinds = append(
		kinds,
		"incident_routing.applied_pagerduty_resource",
		"incident_routing.observed_pagerduty_service",
		"incident_routing.coverage_warning",
	)
	return kinds
}

// buildStoryTargetSupport turns the rows the SQL returned into the support
// block. It re-checks in Go what the SQL selected on, so a row that does not
// carry the target repository's link is never evidence whatever reached it, and
// it applies the graph gate a service target needs: repository-linked rows are
// evidence only when the graph showed the repository defining exactly the
// target, ambiguous when it defines several workloads including the target, and
// dropped otherwise.
func buildStoryTargetSupport(
	filter serviceStoryTargetSupportFilter,
	facts []map[string]any,
	truncated bool,
) map[string]any {
	repoID := storySupportLinkRepositoryID(filter)
	basis, ambiguousBasis := storySupportLinkBases(filter)
	evidence := make([]map[string]any, 0, len(facts))
	ambiguous := make([]map[string]any, 0)
	for _, fact := range facts {
		if repoID == "" || !storySupportFactLinkedToRepository(fact, repoID) {
			continue
		}
		switch {
		case basis != "":
			evidence = append(evidence, serviceStorySupportEvidenceRow(fact, basis))
		case ambiguousBasis != "":
			ambiguous = append(ambiguous, serviceStorySupportEvidenceRow(fact, ambiguousBasis))
		}
	}
	coverage := map[string]any{
		"target":            serviceStoryTargetSupportScopeMap(filter),
		"target_fact_count": len(evidence) + len(ambiguous),
		"truncated":         truncated,
	}
	if strings.TrimSpace(filter.TargetKind) == "service" {
		coverage["repository_workload_count"] = filter.RepositoryWorkloadCount
	}
	return map[string]any{
		"evidence":               evidence,
		"evidence_count":         len(evidence),
		"work_item_count":        serviceStorySupportFamilyCount(evidence, "work_item."),
		"incident_routing_count": serviceStorySupportFamilyCount(evidence, "incident_routing."),
		"ambiguous_evidence":     ambiguous,
		"ambiguous_count":        len(ambiguous),
		"coverage":               coverage,
		"missing_evidence":       serviceStorySupportMissingEvidence(filter, evidence, ambiguous),
		"limit":                  serviceStoryTargetSupportRowLimit(filter.Limit),
		"source":                 "support_read_model",
	}
}

// storySupportLinkBases names how a repository-linked row attaches to the
// target: the evidence basis, or the ambiguous basis when the row cannot be
// attributed to one target. Both are empty when the row attaches to nothing.
// A repository target owns its own links. A service target owns them only when
// the graph shows its repository defining exactly that workload, and shares
// them when the repository defines several workloads including it.
func storySupportLinkBases(filter serviceStoryTargetSupportFilter) (evidence, ambiguous string) {
	switch strings.TrimSpace(filter.TargetKind) {
	case "repository":
		return storySupportBasisLinkedRepository, ""
	case "service":
		if !filter.RepositoryDefinesTarget {
			return "", ""
		}
		switch {
		case filter.RepositoryWorkloadCount == 1:
			return storySupportBasisSoleWorkload, ""
		case filter.RepositoryWorkloadCount > 1:
			return "", storySupportBasisSharedRepository
		}
	}
	return "", ""
}

// storySupportFactLinkedToRepository reports whether fact is a support link
// kind whose durable linked_repository_id is exactly repoID.
func storySupportFactLinkedToRepository(fact map[string]any, repoID string) bool {
	if StringVal(fact, "fact_kind") != storySupportLinkFactKind {
		return false
	}
	return strings.TrimSpace(StringVal(mapValue(fact, "payload"), storySupportLinkPayloadKey)) == repoID
}

func serviceStorySupportEvidenceRow(fact map[string]any, linkBasis string) map[string]any {
	row := map[string]any{
		"link_basis":    linkBasis,
		"fact_id":       StringVal(fact, "fact_id"),
		"fact_kind":     StringVal(fact, "fact_kind"),
		"scope_id":      StringVal(fact, "scope_id"),
		"generation_id": StringVal(fact, "generation_id"),
		"source_system": StringVal(fact, "source_system"),
		"observed_at":   StringVal(fact, "observed_at"),
	}
	if sourceRecordID := StringVal(fact, "source_record_id"); sourceRecordID != "" {
		row["source_record_id"] = sourceRecordID
	}
	if payload := serviceStorySupportEvidencePayload(mapValue(fact, "payload")); len(payload) > 0 {
		row["payload"] = payload
	}
	return row
}

func serviceStorySupportEvidencePayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	allowed := []string{
		"provider",
		"evidence_state",
		"work_item_key",
		"provider_work_item_id",
		"project_key",
		"issue_type_name",
		"status_name",
		"provider_changelog_id",
		"field",
		"value_redacted",
		"provider_remote_link_id",
		"linked_repository_id",
		"application_name",
		"application_type",
		"relationship",
		"url_fingerprint",
		"url_present",
		"url_redacted",
		"title_present",
		"summary_present",
		"correlation_anchor_class",
		"provider_support_state",
		"redaction_policy_version",
		"source_class",
		"source_kind",
		"outcome",
		"resource_class",
		"provider_object_id",
		"service_id",
		"status",
		"declared_match_state",
		"drift_candidate_reason",
		"redaction_state",
		"reason",
	}
	out := map[string]any{}
	for _, key := range allowed {
		if value, ok := payload[key]; ok {
			out[key] = value
		}
	}
	return out
}

func serviceStorySupportFamilyCount(rows []map[string]any, prefix string) int {
	count := 0
	for _, row := range rows {
		if strings.HasPrefix(StringVal(row, "fact_kind"), prefix) {
			count++
		}
	}
	return count
}

func serviceStoryTargetSupportScopeMap(filter serviceStoryTargetSupportFilter) map[string]any {
	return map[string]any{
		"repository":  strings.TrimSpace(filter.Repository),
		"target_kind": strings.TrimSpace(filter.TargetKind),
		"target_id":   strings.TrimSpace(filter.TargetID),
		"service_id":  strings.TrimSpace(filter.ServiceID),
	}
}

func serviceStorySupportMissingEvidence(
	filter serviceStoryTargetSupportFilter,
	evidence []map[string]any,
	ambiguous []map[string]any,
) []map[string]any {
	if len(evidence) > 0 {
		return []map[string]any{}
	}
	if len(ambiguous) > 0 {
		return []map[string]any{{
			"reason": "support_correlation_ambiguous",
			"detail": fmt.Sprintf(
				"the repository that links this support defines %s, so its links cannot be attributed to the selected service",
				storySupportWorkloadCountPhrase(filter.RepositoryWorkloadCount),
			),
		}}
	}
	if !querycontract.DocumentationTargetScopeHasSelector(
		querycontract.DocumentationTargetScopeFromValues(filter.Repository, filter.TargetKind, filter.TargetID, filter.ServiceID),
	) {
		return []map[string]any{}
	}
	return []map[string]any{{
		"reason": "support_target_facts_absent",
		"detail": "no collected Jira or PagerDuty support fact carries a durable link to the selected target",
	}}
}

// storySupportWorkloadCountPhrase words the workload count the DEFINES read
// found. The read is bounded, so a count at the bound means "at least".
func storySupportWorkloadCountPhrase(count int) string {
	if count >= querycontract.ServiceStoryRepositoryWorkloadReadLimit {
		return fmt.Sprintf("at least %d workloads", count)
	}
	return fmt.Sprintf("%d workloads", count)
}

func serviceStoryTargetSupportRowLimit(limit int) int {
	if limit <= 0 || limit > serviceStoryTargetSupportLimit {
		return serviceStoryTargetSupportLimit
	}
	return limit
}
