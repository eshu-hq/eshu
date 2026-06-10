package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func (cr *ContentReader) documentationEvidencePacketWithFilter(
	ctx context.Context,
	filter documentationEvidencePacketFilter,
) (documentationEvidencePacketReadModel, error) {
	if cr == nil || cr.db == nil || strings.TrimSpace(filter.FindingID) == "" {
		return documentationEvidencePacketReadModel{}, nil
	}
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "get_documentation_evidence_packet"),
			attribute.String("db.sql.table", "fact_records"),
		),
	)
	defer span.End()

	query, args := buildDocumentationEvidencePacketByFindingSQL(filter)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return documentationEvidencePacketReadModel{}, fmt.Errorf("query documentation evidence packet: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			span.RecordError(err)
			return documentationEvidencePacketReadModel{}, fmt.Errorf("query documentation evidence packet: %w", err)
		}
		return documentationEvidencePacketReadModel{}, nil
	}
	packet, err := scanJSONPayload(rows)
	if err != nil {
		span.RecordError(err)
		return documentationEvidencePacketReadModel{}, fmt.Errorf("query documentation evidence packet: %w", err)
	}
	if documentationPayloadDenied(packet) {
		return documentationEvidencePacketReadModel{
			Denied:       true,
			DeniedReason: documentationPermissionReason(packet),
		}, nil
	}
	return documentationEvidencePacketReadModel{Available: true, Packet: packet}, nil
}

func (cr *ContentReader) documentationEvidencePacketFreshnessWithFilter(
	ctx context.Context,
	filter documentationEvidencePacketFreshnessFilter,
) (documentationEvidencePacketFreshnessReadModel, error) {
	if cr == nil || cr.db == nil || strings.TrimSpace(filter.PacketID) == "" {
		return documentationEvidencePacketFreshnessReadModel{}, nil
	}
	ctx, span := cr.tracer.Start(ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "check_documentation_packet_freshness"),
			attribute.String("db.sql.table", "fact_records"),
		),
	)
	defer span.End()

	query, args := buildDocumentationEvidencePacketByPacketSQL(filter)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		return documentationEvidencePacketFreshnessReadModel{}, fmt.Errorf("query documentation evidence packet freshness: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			span.RecordError(err)
			return documentationEvidencePacketFreshnessReadModel{}, fmt.Errorf("query documentation evidence packet freshness: %w", err)
		}
		return documentationEvidencePacketFreshnessReadModel{}, nil
	}
	packet, err := scanJSONPayload(rows)
	if err != nil {
		span.RecordError(err)
		return documentationEvidencePacketFreshnessReadModel{}, fmt.Errorf("query documentation evidence packet freshness: %w", err)
	}
	if documentationPayloadDenied(packet) {
		return documentationEvidencePacketFreshnessReadModel{
			Denied:       true,
			DeniedReason: documentationPermissionReason(packet),
		}, nil
	}
	latestPacketVersion := stringFromMap(packet, "packet_version")
	packetVersion := strings.TrimSpace(filter.SavedPacketVersion)
	if packetVersion == "" {
		packetVersion = latestPacketVersion
	}
	freshnessState := nestedString(packet, "states", "freshness_state")
	if packetVersion != latestPacketVersion {
		freshnessState = string(FreshnessStale)
	}
	return documentationEvidencePacketFreshnessReadModel{
		Available:           true,
		PacketID:            stringFromMap(packet, "packet_id"),
		PacketVersion:       packetVersion,
		FreshnessState:      freshnessState,
		LatestPacketVersion: latestPacketVersion,
	}, nil
}

func buildDocumentationEvidencePacketByFindingSQL(filter documentationEvidencePacketFilter) (string, []any) {
	args := []any{strings.TrimSpace(filter.FindingID)}
	clauses := documentationEvidencePacketBaseClauses()
	clauses = append(clauses, fmt.Sprintf("COALESCE(fact_records.payload->>'finding_id', fact_records.payload->'finding'->>'finding_id') = $%d", len(args)))
	clauses, args = appendDocumentationAuthorizationClause(
		clauses,
		args,
		"fact_records",
		"ingestion_scopes",
		filter.AllowedRepositoryIDs,
		filter.AllowedScopeIDs,
	)
	return documentationEvidencePacketSQL(filter.AllowedRepositoryIDs, filter.AllowedScopeIDs, clauses), args
}

func buildDocumentationEvidencePacketByPacketSQL(filter documentationEvidencePacketFreshnessFilter) (string, []any) {
	args := []any{strings.TrimSpace(filter.PacketID)}
	clauses := documentationEvidencePacketBaseClauses()
	clauses = append(clauses, fmt.Sprintf("fact_records.payload->>'packet_id' = $%d", len(args)))
	clauses, args = appendDocumentationAuthorizationClause(
		clauses,
		args,
		"fact_records",
		"ingestion_scopes",
		filter.AllowedRepositoryIDs,
		filter.AllowedScopeIDs,
	)
	return documentationEvidencePacketSQL(filter.AllowedRepositoryIDs, filter.AllowedScopeIDs, clauses), args
}

func documentationEvidencePacketBaseClauses() []string {
	return []string{
		"fact_records.fact_kind = '" + facts.DocumentationEvidencePacketFactKind + "'",
		"fact_records.is_tombstone = FALSE",
	}
}

func documentationEvidencePacketSQL(
	allowedRepositoryIDs []string,
	allowedScopeIDs []string,
	clauses []string,
) string {
	scopeJoin := ""
	if documentationAuthorizationApplies(allowedRepositoryIDs, allowedScopeIDs) {
		scopeJoin = "\nLEFT JOIN ingestion_scopes ON ingestion_scopes.scope_id = fact_records.scope_id"
	}
	return fmt.Sprintf(`
SELECT fact_records.payload
FROM fact_records%s
WHERE %s
ORDER BY fact_records.observed_at DESC, fact_records.fact_id DESC
LIMIT 1
`, scopeJoin, strings.Join(clauses, " AND "))
}
