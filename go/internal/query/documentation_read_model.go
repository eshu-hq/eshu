package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// documentationFindings returns durable documentation findings from fact_records.
func (cr *ContentReader) documentationFindings(
	ctx context.Context,
	filter documentationFindingFilter,
) (documentationFindingListReadModel, error) {
	if cr == nil || cr.db == nil {
		return documentationFindingListReadModel{}, nil
	}
	query, args := buildDocumentationFindingsSQL(filter)
	rows, err := cr.db.QueryContext(ctx, query, args...)
	if err != nil {
		return documentationFindingListReadModel{}, fmt.Errorf("query documentation findings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	findings := make([]map[string]any, 0, limit)
	for rows.Next() {
		payload, err := scanJSONPayload(rows)
		if err != nil {
			return documentationFindingListReadModel{}, fmt.Errorf("query documentation findings: %w", err)
		}
		ensureEvidencePacketURL(payload)
		findings = append(findings, payload)
	}
	if err := rows.Err(); err != nil {
		return documentationFindingListReadModel{}, fmt.Errorf("query documentation findings: %w", err)
	}
	nextCursor := ""
	if len(findings) > limit {
		findings = findings[:limit]
		nextCursor = strconv.Itoa(documentationCursorOffset(filter.Cursor) + limit)
	}
	return documentationFindingListReadModel{Findings: findings, NextCursor: nextCursor}, nil
}

// documentationEvidencePacket returns the latest packet for one finding.
func (cr *ContentReader) documentationEvidencePacket(
	ctx context.Context,
	findingID string,
) (documentationEvidencePacketReadModel, error) {
	if cr == nil || cr.db == nil || strings.TrimSpace(findingID) == "" {
		return documentationEvidencePacketReadModel{}, nil
	}
	rows, err := cr.db.QueryContext(ctx, documentationEvidencePacketByFindingSQL, findingID)
	if err != nil {
		return documentationEvidencePacketReadModel{}, fmt.Errorf("query documentation evidence packet: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return documentationEvidencePacketReadModel{}, fmt.Errorf("query documentation evidence packet: %w", err)
		}
		return documentationEvidencePacketReadModel{}, nil
	}
	packet, err := scanJSONPayload(rows)
	if err != nil {
		return documentationEvidencePacketReadModel{}, fmt.Errorf("query documentation evidence packet: %w", err)
	}
	if documentationPacketDenied(packet) {
		return documentationEvidencePacketReadModel{
			Denied:       true,
			DeniedReason: documentationPermissionReason(packet),
		}, nil
	}
	return documentationEvidencePacketReadModel{Available: true, Packet: packet}, nil
}

// documentationEvidencePacketFreshness returns freshness metadata for one packet.
func (cr *ContentReader) documentationEvidencePacketFreshness(
	ctx context.Context,
	packetID string,
) (documentationEvidencePacketFreshnessReadModel, error) {
	if cr == nil || cr.db == nil || strings.TrimSpace(packetID) == "" {
		return documentationEvidencePacketFreshnessReadModel{}, nil
	}
	rows, err := cr.db.QueryContext(ctx, documentationEvidencePacketByPacketSQL, packetID)
	if err != nil {
		return documentationEvidencePacketFreshnessReadModel{}, fmt.Errorf("query documentation evidence packet freshness: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return documentationEvidencePacketFreshnessReadModel{}, fmt.Errorf("query documentation evidence packet freshness: %w", err)
		}
		return documentationEvidencePacketFreshnessReadModel{}, nil
	}
	packet, err := scanJSONPayload(rows)
	if err != nil {
		return documentationEvidencePacketFreshnessReadModel{}, fmt.Errorf("query documentation evidence packet freshness: %w", err)
	}
	if documentationPacketDenied(packet) {
		return documentationEvidencePacketFreshnessReadModel{
			Denied:       true,
			DeniedReason: documentationPermissionReason(packet),
		}, nil
	}
	packetVersion := stringFromMap(packet, "packet_version")
	return documentationEvidencePacketFreshnessReadModel{
		Available:           true,
		PacketID:            stringFromMap(packet, "packet_id"),
		PacketVersion:       packetVersion,
		FreshnessState:      nestedString(packet, "states", "freshness_state"),
		LatestPacketVersion: packetVersion,
	}, nil
}

const documentationEvidencePacketByFindingSQL = `
SELECT payload
FROM fact_records
WHERE fact_kind = '` + facts.DocumentationEvidencePacketFactKind + `'
  AND is_tombstone = FALSE
  AND (
    payload->>'finding_id' = $1 OR
    payload->'finding'->>'finding_id' = $1
  )
ORDER BY observed_at DESC, fact_id DESC
LIMIT 1
`

const documentationEvidencePacketByPacketSQL = `
SELECT payload
FROM fact_records
WHERE fact_kind = '` + facts.DocumentationEvidencePacketFactKind + `'
  AND is_tombstone = FALSE
  AND payload->>'packet_id' = $1
ORDER BY observed_at DESC, fact_id DESC
LIMIT 1
`

func buildDocumentationFindingsSQL(filter documentationFindingFilter) (string, []any) {
	args := []any{}
	clauses := []string{
		"fact_kind = '" + facts.DocumentationFindingFactKind + "'",
		"is_tombstone = FALSE",
	}
	addPayloadFilter := func(field string, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("payload->>'%s' = $%d", field, len(args)))
	}
	addPayloadFilter("finding_type", filter.FindingType)
	addPayloadFilter("source_id", filter.SourceID)
	addPayloadFilter("document_id", filter.DocumentID)
	addPayloadFilter("status", filter.Status)
	addPayloadFilter("truth_level", filter.TruthLevel)
	addPayloadFilter("freshness_state", filter.FreshnessState)
	if filter.UpdatedSince != nil {
		args = append(args, *filter.UpdatedSince)
		clauses = append(clauses, fmt.Sprintf("observed_at >= $%d", len(args)))
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	args = append(args, limit+1, documentationCursorOffset(filter.Cursor))
	return fmt.Sprintf(`
SELECT payload
FROM fact_records
WHERE %s
ORDER BY observed_at DESC, fact_id DESC
LIMIT $%d OFFSET $%d
`, strings.Join(clauses, " AND "), len(args)-1, len(args)), args
}

func scanJSONPayload(rows *sql.Rows) (map[string]any, error) {
	var raw []byte
	if err := rows.Scan(&raw); err != nil {
		return nil, err
	}
	payload := map[string]any{}
	if len(raw) == 0 {
		return payload, nil
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode payload JSON: %w", err)
	}
	return payload, nil
}

func ensureEvidencePacketURL(finding map[string]any) {
	if stringFromMap(finding, "evidence_packet_url") != "" {
		return
	}
	findingID := stringFromMap(finding, "finding_id")
	if findingID == "" {
		return
	}
	finding["evidence_packet_url"] = "/api/v0/documentation/findings/" + findingID + "/evidence-packet"
}

func documentationPacketDenied(packet map[string]any) bool {
	if canRead, ok := nestedBoolValue(packet, "permissions", "viewer_can_read_source"); ok && !canRead {
		return true
	}
	return strings.EqualFold(nestedString(packet, "states", "permission_decision"), "denied")
}

func documentationPermissionReason(packet map[string]any) string {
	if reason := nestedString(packet, "permissions", "denied_reason"); reason != "" {
		return reason
	}
	if reason := nestedString(packet, "states", "permission_reason"); reason != "" {
		return reason
	}
	return "caller cannot view documentation evidence"
}

func stringFromMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func nestedString(values map[string]any, objectKey, valueKey string) string {
	nested, _ := values[objectKey].(map[string]any)
	value, _ := nested[valueKey].(string)
	return strings.TrimSpace(value)
}

func nestedBoolValue(values map[string]any, objectKey, valueKey string) (bool, bool) {
	nested, _ := values[objectKey].(map[string]any)
	value, ok := nested[valueKey].(bool)
	return value, ok
}
