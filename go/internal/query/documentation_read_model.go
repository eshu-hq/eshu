// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// documentationFindings returns durable documentation findings from fact_records.
func (cr *ContentReader) documentationFindings(
	ctx context.Context,
	filter documentationFindingFilter,
) (documentationFindingListReadModel, error) {
	if cr == nil || cr.db == nil {
		return documentationFindingListReadModel{}, nil
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_documentation_findings"),
			attribute.String("db.sql.table", "fact_records"),
		),
	)
	defer span.End()

	query, args := buildDocumentationFindingsSQL(filter)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return documentationFindingListReadModel{}, fmt.Errorf("query documentation findings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	findings := make([]map[string]any, 0, limit)
	var disclosure sourceACLDisclosureCounts
	for rows.Next() {
		payload, err := scanJSONPayload(rows)
		if err != nil {
			span.RecordError(err)
			return documentationFindingListReadModel{}, fmt.Errorf("query documentation findings: %w", err)
		}
		// Honest source-ACL disclosure (#2164 USER-APPROVED policy): instead of
		// silently dropping a finding the caller cannot read, surface it with an
		// access-denied/partial/stale disposition and the protected content
		// withheld. The binary per-caller read decision is the authoritative
		// authorization axis; the bounded source_acl_state drives the
		// partial/stale/missing markers. A missing source is disclosed as missing
		// (callers treat it as empty). The existing freshness/truth labels (#2138)
		// are preserved and never collapsed into the permission error.
		readable := binaryReadableFromPermissions(payload)
		if readable {
			// Only attach the evidence-packet URL for rows whose content is not
			// withheld; the URL points at the protected packet body.
			ensureEvidencePacketURL(payload)
		}
		disp := applySourceACLDisclosure(payload, readable)
		disclosure.record(disp.disposition)
		findings = append(findings, payload)
	}
	disclosure.annotateSpan(span)
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return documentationFindingListReadModel{}, fmt.Errorf("query documentation findings: %w", err)
	}
	nextCursor := ""
	if len(findings) > limit {
		findings = findings[:limit]
		nextCursor = strconv.Itoa(filter.Offset + limit)
	}
	readModel := documentationFindingListReadModel{Findings: findings, NextCursor: nextCursor}
	if documentationTargetScopeFromFindingFilter(filter).hasSelector() {
		relatedFacts, truncated, err := cr.documentationTargetFacts(ctx, filter)
		if err != nil {
			span.RecordError(err)
			return documentationFindingListReadModel{}, err
		}
		readModel.RelatedFacts = relatedFacts
		readModel.Coverage = documentationTargetCoverageFromFacts(filter, findings, relatedFacts, truncated)
		if readModel.Coverage.FindingsReturned == 0 && readModel.Coverage.TargetFactCount == 0 {
			sourceOnlyCoverage, err := cr.documentationSourceOnlySummary(ctx, filter)
			if err != nil {
				span.RecordError(err)
				return documentationFindingListReadModel{}, err
			}
			readModel.Coverage.SourceOnlyCount = sourceOnlyCoverage.SourceOnlyCount
			readModel.Coverage.SourceOnlyFactKinds = sourceOnlyCoverage.SourceOnlyFactKinds
		}
		readModel.MissingEvidence = documentationMissingEvidenceForTarget(readModel.Coverage)
	}
	return readModel, nil
}

// documentationFacts returns collected documentation facts from fact_records.
func (cr *ContentReader) documentationFacts(
	ctx context.Context,
	filter documentationFactFilter,
) (documentationFactListReadModel, error) {
	if cr == nil || cr.db == nil {
		return documentationFactListReadModel{}, nil
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "list_documentation_facts"),
			attribute.String("db.sql.table", "fact_records"),
		),
	)
	defer span.End()

	query, args := buildDocumentationFactsSQL(filter)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return documentationFactListReadModel{}, fmt.Errorf("query documentation facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	factRows := make([]map[string]any, 0, limit)
	for rows.Next() {
		payload, err := scanJSONPayload(rows)
		if err != nil {
			span.RecordError(err)
			return documentationFactListReadModel{}, fmt.Errorf("query documentation facts: %w", err)
		}
		factRows = append(factRows, payload)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		return documentationFactListReadModel{}, fmt.Errorf("query documentation facts: %w", err)
	}
	nextCursor := ""
	if len(factRows) > limit {
		factRows = factRows[:limit]
		nextCursor = strconv.Itoa(filter.Offset + limit)
	}
	return documentationFactListReadModel{Facts: factRows, NextCursor: nextCursor}, nil
}

// documentationEvidencePacket returns the latest packet for one finding.
func (cr *ContentReader) documentationEvidencePacket(
	ctx context.Context,
	findingID string,
) (documentationEvidencePacketReadModel, error) {
	return cr.documentationEvidencePacketWithFilter(ctx, documentationEvidencePacketFilter{FindingID: findingID})
}

// documentationEvidencePacketFreshness returns freshness metadata for one packet.
func (cr *ContentReader) documentationEvidencePacketFreshness(
	ctx context.Context,
	packetID string,
	savedPacketVersion string,
) (documentationEvidencePacketFreshnessReadModel, error) {
	return cr.documentationEvidencePacketFreshnessWithFilter(ctx, documentationEvidencePacketFreshnessFilter{
		PacketID:           packetID,
		SavedPacketVersion: savedPacketVersion,
	})
}

func buildDocumentationFindingsSQL(filter documentationFindingFilter) (string, []any) {
	args := []any{}
	// The per-caller content-visibility predicates (viewer_can_read_source,
	// source_acl_evaluated, permission_decision) are intentionally NOT applied as
	// a silent SQL drop anymore (#2164 USER-APPROVED disclosure policy). Rows that
	// fail those predicates are returned with content withheld and an access-denied
	// disposition so a reader can distinguish "no evidence" from "evidence exists
	// but is denied/partial/stale," instead of being filtered to clean "nothing
	// found." Withholding is enforced in Go by applyDocumentationFindingDisclosure;
	// the cross-tenant authorization clause below is a distinct boundary and stays.
	clauses := []string{
		"fact_records.fact_kind = '" + facts.DocumentationFindingFactKind + "'",
		"fact_records.is_tombstone = FALSE",
	}
	addColumnFilter := func(field string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("%s = $%d", field, len(args)))
	}
	addPayloadFilter := func(field string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("fact_records.payload->>'%s' = $%d", field, len(args)))
	}
	addColumnFilter("fact_records.scope_id", filter.ScopeID)
	addColumnFilter("fact_records.generation_id", filter.GenerationID)
	repository := strings.TrimSpace(filter.Repository)
	if repository != "" {
		args = append(args, repository)
		sourceRepoPredicate := fmt.Sprintf("ingestion_scopes.payload->>'repo' = $%d", len(args))
		targetPredicate, nextArgs := documentationTargetPredicate(
			args,
			"fact_records.payload",
			documentationTargetRefsFromFindingFilter(filter),
		)
		args = nextArgs
		if targetPredicate == "" {
			clauses = append(clauses, sourceRepoPredicate)
		} else {
			clauses = append(clauses, "("+sourceRepoPredicate+" OR "+targetPredicate+")")
		}
	} else {
		clauses, args = appendDocumentationTargetClause(
			clauses,
			args,
			"fact_records.payload",
			documentationTargetRefsFromFindingFilter(filter),
		)
	}
	addPayloadFilter("finding_type", filter.FindingType)
	addPayloadFilter("source_id", filter.SourceID)
	addPayloadFilter("document_id", filter.DocumentID)
	addPayloadFilter("status", filter.Status)
	addPayloadFilter("truth_level", filter.TruthLevel)
	addPayloadFilter("freshness_state", filter.FreshnessState)
	if filter.UpdatedSince != nil {
		args = append(args, *filter.UpdatedSince)
		clauses = append(clauses, fmt.Sprintf("fact_records.observed_at >= $%d", len(args)))
	}
	clauses, args = appendDocumentationAuthorizationClause(
		clauses,
		args,
		"fact_records",
		"ingestion_scopes",
		filter.AllowedRepositoryIDs,
		filter.AllowedScopeIDs,
	)
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit+1, filter.Offset)
	return fmt.Sprintf(`
SELECT fact_records.payload
    || jsonb_build_object('scope_id', fact_records.scope_id, 'generation_id', fact_records.generation_id)
    || CASE
        WHEN ingestion_scopes.payload ? 'repo'
            THEN jsonb_build_object('repo', ingestion_scopes.payload->>'repo')
        ELSE '{}'::jsonb
    END AS payload
FROM fact_records
LEFT JOIN ingestion_scopes ON ingestion_scopes.scope_id = fact_records.scope_id
WHERE %s
ORDER BY fact_records.observed_at DESC, fact_records.fact_id DESC
LIMIT $%d OFFSET $%d
`, strings.Join(clauses, " AND "), len(args)-1, len(args)), args
}

func buildDocumentationFactsSQL(filter documentationFactFilter) (string, []any) {
	args := []any{}
	clauses := []string{
		"fact_records.is_tombstone = FALSE",
	}
	addColumnFilter := func(field string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("%s = $%d", field, len(args)))
	}
	addPayloadFilter := func(field string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("fact_records.payload->>'%s' = $%d", field, len(args)))
	}
	if strings.TrimSpace(filter.FactKind) != "" {
		addColumnFilter("fact_records.fact_kind", filter.FactKind)
	} else {
		clauses = append(clauses, "fact_records.fact_kind IN ("+documentationCollectedFactKindSQLList()+")")
	}
	addColumnFilter("fact_records.scope_id", filter.ScopeID)
	addColumnFilter("fact_records.generation_id", filter.GenerationID)
	clauses, args = appendDocumentationTargetClause(
		clauses,
		args,
		"fact_records.payload",
		documentationTargetRefsFromFactFilter(filter),
	)
	addPayloadFilter("source_id", filter.SourceID)
	addPayloadFilter("document_id", filter.DocumentID)
	addPayloadFilter("section_id", filter.SectionID)
	if strings.TrimSpace(filter.Query) != "" {
		args = append(args, "%"+strings.ToLower(strings.TrimSpace(filter.Query))+"%")
		clauses = append(clauses, fmt.Sprintf(`LOWER(
			COALESCE(fact_records.payload->>'display_name', '') || ' ' ||
			COALESCE(fact_records.payload->>'title', '') || ' ' ||
			COALESCE(fact_records.payload->>'heading_text', '') || ' ' ||
			COALESCE(fact_records.payload->>'content', '') || ' ' ||
			COALESCE(fact_records.payload->>'target_uri', '')
		) LIKE $%d`, len(args)))
	}
	if filter.UpdatedSince != nil {
		args = append(args, *filter.UpdatedSince)
		clauses = append(clauses, fmt.Sprintf("fact_records.observed_at >= $%d", len(args)))
	}
	clauses, args = appendDocumentationAuthorizationClause(
		clauses,
		args,
		"fact_records",
		"ingestion_scopes",
		filter.AllowedRepositoryIDs,
		filter.AllowedScopeIDs,
	)
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	scopeJoin := ""
	if documentationAuthorizationApplies(filter.AllowedRepositoryIDs, filter.AllowedScopeIDs) {
		scopeJoin = "\nLEFT JOIN ingestion_scopes ON ingestion_scopes.scope_id = fact_records.scope_id"
	}
	args = append(args, limit+1, filter.Offset)
	return fmt.Sprintf(`
SELECT jsonb_build_object(
    'fact_id', fact_records.fact_id,
    'fact_kind', fact_records.fact_kind,
    'scope_id', fact_records.scope_id,
    'generation_id', fact_records.generation_id,
    'source_system', fact_records.source_system,
    'source_uri', fact_records.source_uri,
    'source_record_id', fact_records.source_record_id,
    'observed_at', fact_records.observed_at,
    'payload', fact_records.payload
) AS payload
FROM fact_records
%s
WHERE %s
ORDER BY fact_records.observed_at DESC, fact_records.fact_id DESC
LIMIT $%d OFFSET $%d
`, scopeJoin, strings.Join(clauses, " AND "), len(args)-1, len(args)), args
}

func documentationCollectedFactKindSQLList() string {
	return "'" + facts.DocumentationSourceFactKind + "', " +
		"'" + facts.DocumentationDocumentFactKind + "', " +
		"'" + facts.DocumentationSectionFactKind + "', " +
		"'" + facts.DocumentationLinkFactKind + "', " +
		"'" + facts.DocumentationEntityMentionFactKind + "', " +
		"'" + facts.DocumentationClaimCandidateFactKind + "', " +
		"'" + facts.SemanticDocumentationObservationFactKind + "'"
}
