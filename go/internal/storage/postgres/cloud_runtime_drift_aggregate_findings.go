// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// CloudRuntimeDriftAggregateFilter bounds an aggregate read that spans BOTH
// the provider-neutral (reducer_multi_cloud_runtime_drift_finding) and
// AWS-specific (reducer_aws_cloud_runtime_drift_finding) fact kinds (issue
// #5759 follow-up). DomainAWSCloudRuntimeDrift stays the sole writer for AWS
// findings and DomainMultiCloudRuntimeDrift's excludeAWSOwnedRows still drops
// every AWS-provider row before publication -- that write-side partition is
// unchanged. This is a READ-side aggregation only: the provider-neutral read
// surface (list_cloud_runtime_drift_findings / POST
// /api/v0/cloud/runtime-drift/findings / export_cloud_runtime_drift_packet)
// advertises coverage across aws, gcp, and azure and must genuinely deliver
// it instead of silently returning an empty page for provider=aws.
type CloudRuntimeDriftAggregateFilter struct {
	ScopeID string
	// Provider optionally restricts the read to one provider.
	//   - "aws" reads ONLY the AWS-specific fact kind. AWS findings carry no
	//     stored provider column (the domain is AWS's sole writer, so
	//     fact_kind alone identifies the provider); no additional predicate
	//     is needed or applied.
	//   - "gcp" or "azure" reads ONLY the provider-neutral fact kind, filtered
	//     by its stored payload->>'provider' column exactly as before this
	//     aggregation existed.
	//   - "" (unfiltered) reads BOTH fact kinds with no provider predicate.
	Provider string
	// CloudResourceUID, when set, is pushed down against the provider-neutral
	// payload's stored cloud_resource_uid column ONLY. AWS-origin findings do
	// not persist a canonical cloud_resource_uid in their payload (their
	// durable identity is the ARN); payload->>'cloud_resource_uid' therefore
	// evaluates to SQL NULL for an AWS row, and "NULL = $n" is never true, so
	// this predicate naturally excludes every AWS row without a fact_kind
	// branch. This is a disclosed, pre-existing limitation, not a regression:
	// AWS findings were entirely absent from this surface before this
	// aggregation existed, so a cloud_resource_uid lookup could never have
	// matched one either. See the query package's cloud_runtime_drift_aggregate.go
	// for the caller-facing note and CloudRuntimeDriftFindingView's
	// "limitations" text.
	CloudResourceUID string
	FindingKinds     []string
	Limit            int
	Offset           int
}

// CloudRuntimeDriftAggregateFindingRow is one active runtime-drift finding
// read from either fact kind, tagged by FactKind so the query layer knows
// which typed field to read. Exactly one of MultiCloud/AWS is populated,
// matching FactKind.
type CloudRuntimeDriftAggregateFindingRow struct {
	// FactKind is MultiCloudRuntimeDriftFindingFactKind or
	// AWSCloudRuntimeDriftFindingFactKind.
	FactKind string
	// MultiCloud is populated when FactKind == MultiCloudRuntimeDriftFindingFactKind.
	MultiCloud MultiCloudRuntimeDriftFindingRow
	// AWS is populated when FactKind == AWSCloudRuntimeDriftFindingFactKind.
	AWS AWSCloudRuntimeDriftFindingRow
}

// ListActiveFindingsAcrossProviders returns one globally ordered page of
// active runtime-drift findings spanning whichever fact kind(s)
// filter.Provider selects. Ordering and pagination are pushed to one SQL
// query (ORDER BY fact.observed_at DESC, fact.fact_id ASC -- the same key
// both single-fact-kind stores already order by) so a deep offset stays
// correct across the merged set instead of requiring the caller to
// over-fetch and re-sort in Go.
func (s MultiCloudRuntimeDriftFindingStore) ListActiveFindingsAcrossProviders(
	ctx context.Context,
	filter CloudRuntimeDriftAggregateFilter,
) ([]CloudRuntimeDriftAggregateFindingRow, error) {
	if s.db == nil {
		return nil, fmt.Errorf("multi cloud runtime drift finding store database is required")
	}
	filter = normalizeCloudRuntimeDriftAggregateFilter(filter)
	if filter.ScopeID == "" {
		return nil, fmt.Errorf("cloud runtime drift aggregate filter requires scope_id")
	}
	query, args := buildCloudRuntimeDriftAggregateQuery(false, filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list active cloud runtime drift findings across providers: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var findings []CloudRuntimeDriftAggregateFindingRow
	for rows.Next() {
		row, err := scanCloudRuntimeDriftAggregateRow(rows)
		if err != nil {
			return nil, err
		}
		findings = append(findings, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active cloud runtime drift findings across providers: %w", err)
	}
	return findings, nil
}

// CountActiveFindingsAcrossProviders returns the total active finding count
// spanning the same fact kind(s) ListActiveFindingsAcrossProviders selects,
// for the same bounded filters.
func (s MultiCloudRuntimeDriftFindingStore) CountActiveFindingsAcrossProviders(
	ctx context.Context,
	filter CloudRuntimeDriftAggregateFilter,
) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("multi cloud runtime drift finding store database is required")
	}
	filter = normalizeCloudRuntimeDriftAggregateFilter(filter)
	if filter.ScopeID == "" {
		return 0, fmt.Errorf("cloud runtime drift aggregate filter requires scope_id")
	}
	query, args := buildCloudRuntimeDriftAggregateQuery(true, filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("count active cloud runtime drift findings across providers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return 0, nil
	}
	var count int
	if err := rows.Scan(&count); err != nil {
		return 0, fmt.Errorf("scan active cloud runtime drift finding count across providers: %w", err)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate active cloud runtime drift finding count across providers: %w", err)
	}
	return count, nil
}

// scanCloudRuntimeDriftAggregateRow scans one result row and decodes its
// payload with the SAME decoder the single-fact-kind stores use
// (decodeMultiCloudRuntimeDriftFindingPayload / decodeAWSCloudRuntimeDriftFindingPayload),
// selected by the row's own fact_kind column so this file never has to
// duplicate either payload's decode logic.
func scanCloudRuntimeDriftAggregateRow(rows Rows) (CloudRuntimeDriftAggregateFindingRow, error) {
	var factKind, factID, scopeID, generationID, sourceSystem string
	var observedAt time.Time
	var payload []byte
	if err := rows.Scan(&factKind, &factID, &scopeID, &generationID, &sourceSystem, &observedAt, &payload); err != nil {
		return CloudRuntimeDriftAggregateFindingRow{}, fmt.Errorf("scan active cloud runtime drift finding across providers: %w", err)
	}
	out := CloudRuntimeDriftAggregateFindingRow{FactKind: factKind}
	switch factKind {
	case MultiCloudRuntimeDriftFindingFactKind:
		row := MultiCloudRuntimeDriftFindingRow{
			FactID: factID, ScopeID: scopeID, GenerationID: generationID,
			SourceSystem: sourceSystem, ObservedAt: observedAt,
		}
		if err := decodeMultiCloudRuntimeDriftFindingPayload(payload, &row); err != nil {
			return CloudRuntimeDriftAggregateFindingRow{}, err
		}
		out.MultiCloud = row
	case AWSCloudRuntimeDriftFindingFactKind:
		row := AWSCloudRuntimeDriftFindingRow{
			FactID: factID, ScopeID: scopeID, GenerationID: generationID,
			SourceSystem: sourceSystem, ObservedAt: observedAt,
		}
		if err := decodeAWSCloudRuntimeDriftFindingPayload(payload, &row); err != nil {
			return CloudRuntimeDriftAggregateFindingRow{}, err
		}
		out.AWS = row
	default:
		return CloudRuntimeDriftAggregateFindingRow{}, fmt.Errorf(
			"unexpected fact_kind %q in cloud runtime drift aggregate read", factKind,
		)
	}
	return out, nil
}

// buildCloudRuntimeDriftAggregateQuery builds the one query that spans
// whichever fact kind(s) filter.Provider selects. The fact_kind set itself
// does most of the provider dispatch: provider=="aws" selects only the AWS
// fact kind (no AWS row will ever carry a different provider), provider in
// {"gcp","azure"} selects only the provider-neutral fact kind (further
// narrowed by its stored payload->>'provider' column, unchanged from the
// pre-aggregation query), and provider=="" selects both kinds with no
// provider predicate -- a genuinely complete, unfiltered read.
func buildCloudRuntimeDriftAggregateQuery(
	countOnly bool,
	filter CloudRuntimeDriftAggregateFilter,
) (string, []any) {
	selectClause := strings.Join([]string{
		"fact.fact_kind",
		"fact.fact_id",
		"fact.scope_id",
		"fact.generation_id",
		"fact.source_system",
		"fact.observed_at",
		"fact.payload",
	}, ",\n    ")
	if countOnly {
		selectClause = "COUNT(*)"
	}

	kinds := cloudRuntimeDriftAggregateFactKinds(filter.Provider)
	args := []any{pq.StringArray(kinds)}
	conditions := []string{
		"fact.fact_kind = ANY($1)",
		"fact.is_tombstone = false",
	}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	conditions = append(conditions, "fact.scope_id = "+addArg(filter.ScopeID))
	if filter.Provider == "gcp" || filter.Provider == "azure" {
		conditions = append(conditions, "fact.payload->>'provider' = "+addArg(filter.Provider))
	}
	if filter.CloudResourceUID != "" {
		conditions = append(conditions, "fact.payload->>'cloud_resource_uid' = "+addArg(filter.CloudResourceUID))
	}
	if len(filter.FindingKinds) > 0 {
		placeholders := make([]string, 0, len(filter.FindingKinds))
		for _, kind := range filter.FindingKinds {
			placeholders = append(placeholders, addArg(kind))
		}
		conditions = append(conditions, "fact.payload->>'finding_kind' IN ("+strings.Join(placeholders, ", ")+")")
	}

	var builder strings.Builder
	_, _ = fmt.Fprintf(&builder, "SELECT\n    %s\n", selectClause)
	builder.WriteString("FROM fact_records AS fact\n")
	builder.WriteString("JOIN ingestion_scopes AS scope\n")
	builder.WriteString("  ON scope.scope_id = fact.scope_id\n")
	builder.WriteString(" AND scope.active_generation_id = fact.generation_id\n")
	builder.WriteString("WHERE ")
	builder.WriteString(strings.Join(conditions, "\n  AND "))
	builder.WriteString("\n")
	if !countOnly {
		limit := addArg(filter.Limit)
		offset := addArg(filter.Offset)
		builder.WriteString("ORDER BY fact.observed_at DESC, fact.fact_id ASC\n")
		_, _ = fmt.Fprintf(&builder, "LIMIT %s OFFSET %s\n", limit, offset)
	}
	return builder.String(), args
}

// cloudRuntimeDriftAggregateFactKinds resolves the closed fact-kind set one
// provider filter value selects. An unrecognized/blank provider selects
// both kinds (the "unfiltered, genuinely complete" case); the caller
// (query.normalizeCloudRuntimeDriftRequest) already rejects any provider
// value outside {"", "aws", "gcp", "azure"} before this is ever reached.
func cloudRuntimeDriftAggregateFactKinds(provider string) []string {
	switch provider {
	case "aws":
		return []string{AWSCloudRuntimeDriftFindingFactKind}
	case "gcp", "azure":
		return []string{MultiCloudRuntimeDriftFindingFactKind}
	default:
		return []string{MultiCloudRuntimeDriftFindingFactKind, AWSCloudRuntimeDriftFindingFactKind}
	}
}

func normalizeCloudRuntimeDriftAggregateFilter(
	filter CloudRuntimeDriftAggregateFilter,
) CloudRuntimeDriftAggregateFilter {
	filter.ScopeID = strings.TrimSpace(filter.ScopeID)
	filter.Provider = strings.ToLower(strings.TrimSpace(filter.Provider))
	filter.CloudResourceUID = strings.TrimSpace(filter.CloudResourceUID)
	filter.FindingKinds = cleanStringSet(filter.FindingKinds)
	if filter.Limit <= 0 {
		filter.Limit = multiCloudRuntimeDriftFindingDefaultLimit
	}
	if filter.Limit > multiCloudRuntimeDriftFindingMaxLimit {
		filter.Limit = multiCloudRuntimeDriftFindingMaxLimit
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}
