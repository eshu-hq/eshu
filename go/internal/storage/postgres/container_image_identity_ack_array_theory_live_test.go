//go:build perf5854_ack

// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestContainerImageIdentityAckAttemptGroupArrayPlanLive(t *testing.T) {
	db := openContainerImageIdentityAckCapabilityProofDB(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	now := time.Date(2026, time.July, 30, 16, 0, 0, 0, time.UTC)
	const (
		owner      = "reducer-5854-ack-array-theory"
		iterations = 200
		batchSize  = 64
	)

	ids := make([]string, batchSize)
	intents := make([]reducer.Intent, batchSize)
	for index := range batchSize {
		scopeID := fmt.Sprintf("repository:5854-ack-array-theory-%02d", index)
		generationID := fmt.Sprintf("generation:5854-ack-array-theory-%02d", index)
		ids[index] = fmt.Sprintf("ack-5854-array-%02d", index)
		intents[index] = reducer.Intent{
			IntentID:     ids[index],
			Domain:       reducer.DomainContainerImageIdentity,
			AttemptCount: 1,
			ClaimEpoch:   1,
		}
		seedContainerImageIdentityAckScope(t, ctx, db, scopeID)
		seedContainerImageIdentityAckGeneration(t, ctx, db, scopeID, generationID)
		seedContainerImageIdentityAckWorkItem(
			t,
			ctx,
			db,
			ids[index],
			scopeID,
			generationID,
			owner,
			now.Add(time.Minute),
			now,
		)
	}
	prepareContainerImageIdentityAckPerformanceTable(t, db)
	baselineQuery, baselineArgs := legacyContainerImageIdentityAckBatchQuery(
		now,
		owner,
		ids,
	)
	groupedQuery, groupedArgs := ackContainerImageIdentityReducerWorkBatchQuery(
		now,
		owner,
		intents,
	)
	before, after := measureContainerImageIdentityAckPerfPair(
		t,
		ctx,
		db,
		iterations,
		func() {
			resetContainerImageIdentityAckPerfClaimed(t, ctx, db, owner, now, ids)
		},
		func() error {
			_, err := db.ExecContext(ctx, baselineQuery, baselineArgs...)
			return err
		},
		func() error {
			_, err := db.ExecContext(ctx, groupedQuery, groupedArgs...)
			return err
		},
	)
	assertContainerImageIdentityAckPerfBudget(
		t,
		"attempt-group array batch64",
		before,
		after,
	)

	plan := explainContainerImageIdentityAckGroupedPlan(
		t,
		ctx,
		db,
		groupedQuery,
		groupedArgs,
		func() {
			resetContainerImageIdentityAckPerfClaimed(t, ctx, db, owner, now, ids)
		},
	)
	assertContainerImageIdentityAckIndexBackedPlan(t, "custom", plan)
	genericPlan := explainContainerImageIdentityAckGroupedGenericPlan(
		t,
		ctx,
		db,
		groupedQuery,
		now,
		owner,
		ids,
		func() {
			resetContainerImageIdentityAckPerfClaimed(t, ctx, db, owner, now, ids)
		},
	)
	assertContainerImageIdentityAckIndexBackedPlan(t, "generic", genericPlan)
	t.Logf(
		"ACKARRAY5854 pairs=%d before_median_us=%.3f before_p95_us=%.3f after_median_us=%.3f after_p95_us=%.3f",
		iterations,
		float64(before.median)/float64(time.Microsecond),
		float64(before.p95)/float64(time.Microsecond),
		float64(after.median)/float64(time.Microsecond),
		float64(after.p95)/float64(time.Microsecond),
	)
}

func explainContainerImageIdentityAckGroupedPlan(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
	args []any,
	reset func(),
) string {
	t.Helper()
	reset()
	rows, err := db.QueryContext(
		ctx,
		"EXPLAIN (ANALYZE, BUFFERS, WAL) "+query,
		args...,
	)
	if err != nil {
		t.Fatalf("EXPLAIN grouped ACK: %v", err)
	}
	defer func() { _ = rows.Close() }()
	return readContainerImageIdentityAckPlanLines(t, rows)
}

func explainContainerImageIdentityAckGroupedGenericPlan(
	t *testing.T,
	ctx context.Context,
	db *sql.DB,
	query string,
	now time.Time,
	owner string,
	ids []string,
	reset func(),
) string {
	t.Helper()
	reset()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("open generic-plan ACK connection: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		t.Fatalf("force generic ACK plan: %v", err)
	}
	const preparedName = "eshu_5854_ack_group_generic"
	if _, err := conn.ExecContext(
		ctx,
		"PREPARE "+preparedName+" (timestamptz, text, text[], integer) AS "+query,
	); err != nil {
		t.Fatalf("prepare generic grouped ACK: %v", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "DEALLOCATE "+preparedName)
	}()

	arrayValues := make([]string, len(ids))
	for index, id := range ids {
		arrayValues[index] = "'" + strings.ReplaceAll(id, "'", "''") + "'"
	}
	explainSQL := fmt.Sprintf(
		"EXPLAIN (ANALYZE, BUFFERS, WAL) EXECUTE %s ('%s', '%s', ARRAY[%s]::text[], 1)",
		preparedName,
		now.Format(time.RFC3339Nano),
		strings.ReplaceAll(owner, "'", "''"),
		strings.Join(arrayValues, ", "),
	)
	rows, err := conn.QueryContext(ctx, explainSQL)
	if err != nil {
		t.Fatalf("EXPLAIN generic grouped ACK: %v", err)
	}
	defer func() { _ = rows.Close() }()
	plan := readContainerImageIdentityAckPlanLines(t, rows)

	var genericPlans int
	var customPlans int
	if err := conn.QueryRowContext(
		ctx,
		`SELECT generic_plans, custom_plans
		 FROM pg_prepared_statements
		 WHERE name = $1`,
		preparedName,
	).Scan(&genericPlans, &customPlans); err != nil {
		t.Fatalf("read generic grouped ACK counters: %v", err)
	}
	if genericPlans != 1 || customPlans != 0 {
		t.Errorf(
			"grouped ACK plan counters = generic %d custom %d, want 1/0",
			genericPlans,
			customPlans,
		)
	}
	return plan
}

func readContainerImageIdentityAckPlanLines(
	t *testing.T,
	rows *sql.Rows,
) string {
	t.Helper()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan grouped ACK plan: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read grouped ACK plan: %v", err)
	}
	return strings.Join(lines, "\n")
}

func assertContainerImageIdentityAckIndexBackedPlan(
	t *testing.T,
	name string,
	plan string,
) {
	t.Helper()
	if !strings.Contains(plan, "Index Scan") ||
		strings.Contains(plan, "Seq Scan on fact_work_items") {
		t.Errorf("%s grouped ACK plan is not index-backed:\n%s", name, plan)
	}
}
