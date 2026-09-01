// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// cloudInventoryWarningFlagRolloutGap marks a response where an account_id/
// project_id/subscription_id-filtered read returned zero resources AND at
// least one canonical reducer_cloud_resource_identity row exists in the same
// provider/access scope whose payload predates the #5238 account_id rollout
// (admitted by the reducer code before that fix landed, so its payload
// carries no "account_id" key at all -- see
// go/internal/reducer/cloud_inventory_admission_writer.go and the "Rollout
// window" note in docs/public/reference/http-api.md). Without this flag, a
// genuine "no such account" zero-row response is byte-identical to "this
// account's data has not been re-admitted since deploy yet", and an operator
// filtering by account_id cannot tell the two apart from the response alone.
const cloudInventoryWarningFlagRolloutGap = "account_alias_rollout_gap"

// cloudInventoryWarningFlagRolloutGapCheckFailed marks a response where the
// rollout-gap disambiguation query itself failed. The primary read already
// succeeded by the time this check runs, so a failure here degrades the
// response (the disambiguation signal is simply absent) rather than failing
// the whole request -- mirroring the existing content-store-coverage-error
// precedent in repository_stats.go, which reports a distinct missing_evidence
// value instead of aborting a request whose primary answer is already known.
const cloudInventoryWarningFlagRolloutGapCheckFailed = "account_alias_rollout_gap_check_failed"

// cloudInventoryRolloutGapWarningFlags computes the bounded warning_flags for
// one readback response. The disambiguation query behind
// cloudInventoryWarningFlagRolloutGap costs a second bounded Postgres round
// trip, so it is gated strictly to the one case an operator can otherwise
// never resolve from the response alone: an account-alias filter
// (account_id/project_id/subscription_id) was supplied AND it matched zero
// resources. Every other call -- the unfiltered hot path, a scope_id-filtered
// call, and any alias-filtered call that already returned rows -- returns nil
// immediately with no extra query, because in every one of those cases the
// response already answers the question (rows are visible, or no alias
// selector was even in play).
func cloudInventoryRolloutGapWarningFlags(
	ctx context.Context,
	store cloudInventoryReadModelStore,
	filter cloudInventoryFilter,
	readModel cloudInventoryListReadModel,
) []string {
	// A non-zero cursor means the caller already paged past the initial
	// results: an empty page here means the requested offset ran past the
	// available rows, not that the account may be missing, so the probe would
	// answer a question the caller did not ask. Restrict to the initial page
	// (offset zero) (#5881 review follow-up).
	if filter.AccountAliasKey == "" || filter.Offset != 0 || len(readModel.Resources) != 0 {
		return nil
	}
	exists, err := store.CloudInventoryPreRolloutEvidenceExists(ctx, filter)
	if err != nil {
		return []string{cloudInventoryWarningFlagRolloutGapCheckFailed}
	}
	if !exists {
		return nil
	}
	return []string{cloudInventoryWarningFlagRolloutGap}
}

// CloudInventoryPreRolloutEvidenceExists implements
// cloudInventoryReadModelStore. It reports whether ANY active-generation
// reducer_cloud_resource_identity row in the filter's provider/access scope
// carries no "account_id" payload key at all -- the exact shape a row
// admitted before the #5238 fix deployed still has until its scope's next
// collector sync re-admits it (cloudInventoryAdmissionFactID never hashed
// AccountID into the fact identity, so a pre-fix row keeps its existing
// fact_id and is never rewritten by the fix alone).
func (cr *ContentReader) CloudInventoryPreRolloutEvidenceExists(
	ctx context.Context,
	filter cloudInventoryFilter,
) (bool, error) {
	if cr == nil || cr.db == nil {
		return false, nil
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "cloud_inventory_pre_rollout_evidence_exists"),
			attribute.String("db.sql.table", "fact_records"),
		),
	)
	defer span.End()

	query, args := buildCloudInventoryPreRolloutProbeSQL(filter)
	var exists bool
	if err := cr.db.QueryRowContext(ctx, query, args...).Scan(&exists); err != nil {
		span.RecordError(err)
		return false, fmt.Errorf("query cloud inventory pre-rollout evidence: %w", err)
	}
	return exists, nil
}

// buildCloudInventoryPreRolloutProbeSQL assembles the bounded EXISTS/LIMIT-1
// probe and its parameters. It mirrors buildCloudInventoryIdentitiesSQL's
// active-generation join and access-scope predicate exactly, so the probe
// only ever sees the same rows the primary read is authorized to see, but
// replaces the account_id equality predicate with its logical negation: "this
// row's payload has no account_id key at all". EXPLAIN ANALYZE against a
// representative corpus (200 active scopes, 100k reducer_cloud_resource_
// identity facts, ~10% of scopes pre-rollout-shaped) showed this probe
// resolves via the same fact_records_collector_status_active_idx the primary
// query already uses -- no sequential scan, no new index required -- in
// under 0.1ms when a match exists nearby, and in the true worst case (no
// matching row anywhere in the provider/access scope, so every active
// generation must be visited before concluding false) costs about the same
// as the primary query's own already-accepted zero-result cost on the same
// corpus (~14.5ms vs ~15ms). See docs/internal/evidence/
// 5238-account-alias-rollout-gap-signal.md for the full EXPLAIN ANALYZE
// output and methodology.
func buildCloudInventoryPreRolloutProbeSQL(filter cloudInventoryFilter) (string, []any) {
	args := []any{}
	clauses := []string{
		"fact_records.fact_kind = '" + cloudInventoryFactKind + "'",
		"fact_records.is_tombstone = FALSE",
		"NOT (fact_records.payload ? 'account_id')",
	}
	if provider := strings.TrimSpace(filter.Provider); provider != "" {
		args = append(args, provider)
		clauses = append(clauses, fmt.Sprintf("fact_records.payload->>'provider' = $%d", len(args)))
	}
	// A management_origin filter narrows which rows the CALLER'S question is
	// even about; the probe must apply the identical predicate the primary
	// query does, or it can find an unrelated-origin pre-fix row and warn
	// account_alias_rollout_gap for a question that row has nothing to do
	// with -- a false warning (#5881 review follow-up).
	if managementOrigin := strings.TrimSpace(filter.ManagementOrigin); managementOrigin != "" {
		args = append(args, managementOrigin)
		clauses = append(clauses, fmt.Sprintf("fact_records.payload->>'management_origin' = $%d", len(args)))
	}
	if !filter.AllScopes {
		args = append(args, pgarray.Array(filter.AllowedRepositoryIDs), pgarray.Array(filter.AllowedScopeIDs))
		clauses = append(clauses, fmt.Sprintf(
			"(fact_records.scope_id = ANY($%d) OR fact_records.scope_id = ANY($%d))",
			len(args)-1, len(args),
		))
	}
	query := fmt.Sprintf(`
SELECT EXISTS (
    SELECT 1
    FROM fact_records
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = fact_records.scope_id
     AND scope.active_generation_id = fact_records.generation_id
    JOIN scope_generations AS generation
      ON generation.scope_id = fact_records.scope_id
     AND generation.generation_id = fact_records.generation_id
     AND generation.status = 'active'
    WHERE %s
    LIMIT 1
)
`, strings.Join(clauses, " AND "))
	return query, args
}
