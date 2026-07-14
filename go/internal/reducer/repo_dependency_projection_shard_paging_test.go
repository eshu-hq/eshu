// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRepoDependencyShardReaderContinuesPastForeignFullPage(t *testing.T) {
	t.Parallel()

	const (
		shardID    = 1
		shardCount = 2
	)
	foreignUnit := repoDependencyUnitForShard(t, 0, shardCount)
	targetUnit := repoDependencyUnitForShard(t, shardID, shardCount)
	now := time.Date(2026, time.July, 14, 9, 0, 0, 0, time.UTC)
	rows := make([]SharedProjectionIntentRow, 0, maxRepoDependencyAcceptanceScanLimit+1)
	for i := range maxRepoDependencyAcceptanceScanLimit {
		rows = append(rows, repoDependencyShardPagingRow(
			fmt.Sprintf("foreign-%05d", i), foreignUnit, now.Add(time.Duration(i)*time.Nanosecond),
		))
	}
	rows = append(rows, repoDependencyShardPagingRow(
		"target-10000", targetUnit, now.Add(maxRepoDependencyAcceptanceScanLimit*time.Nanosecond),
	))

	inner := &pagedRepoDependencyIntentReader{rows: rows}
	sharded := &repoDependencyShardReader{inner: inner, shardID: shardID, shardCount: shardCount}
	runner := RepoDependencyProjectionRunner{
		IntentReader: sharded,
		AcceptedGen: func(key SharedProjectionAcceptanceKey) (string, bool) {
			if key.AcceptanceUnitID == targetUnit {
				return "generation-target", true
			}
			return "", false
		},
		Config: RepoDependencyProjectionRunnerConfig{BatchLimit: 100},
	}

	got, err := runner.selectAcceptanceUnitWork(context.Background())
	if err != nil {
		t.Fatalf("selectAcceptanceUnitWork() error = %v", err)
	}
	if got != targetUnit {
		t.Fatalf("selected acceptance unit = %q, want %q beyond the foreign 10k prefix", got, targetUnit)
	}
	if inner.continuationCalls == 0 {
		t.Fatal("shard reader did not continue after a full foreign page")
	}
}

func TestRepoDependencyShardReaderFailsClosedWithoutContinuation(t *testing.T) {
	t.Parallel()

	const (
		shardID    = 1
		shardCount = 2
	)
	foreignUnit := repoDependencyUnitForShard(t, 0, shardCount)
	now := time.Date(2026, time.July, 14, 9, 0, 0, 0, time.UTC)
	rows := make([]SharedProjectionIntentRow, 0, maxRepoDependencyAcceptanceScanLimit)
	for i := range maxRepoDependencyAcceptanceScanLimit {
		rows = append(rows, repoDependencyShardPagingRow(
			fmt.Sprintf("foreign-%05d", i), foreignUnit, now.Add(time.Duration(i)*time.Nanosecond),
		))
	}

	sharded := &repoDependencyShardReader{
		inner:      &nonPagedRepoDependencyIntentReader{rows: rows},
		shardID:    shardID,
		shardCount: shardCount,
	}
	if _, err := sharded.ListPendingDomainIntents(context.Background(), DomainRepoDependency, 100); err == nil {
		t.Fatal("full foreign page without continuation returned a false empty result; want fail-closed error")
	}
}

func TestRepoDependencyShardReaderRejectsNonAdvancingContinuation(t *testing.T) {
	t.Parallel()

	const (
		shardID    = 1
		shardCount = 2
	)
	foreignUnit := repoDependencyUnitForShard(t, 0, shardCount)
	now := time.Date(2026, time.July, 14, 9, 0, 0, 0, time.UTC)
	rows := make([]SharedProjectionIntentRow, 0, maxRepoDependencyAcceptanceScanLimit)
	for i := range maxRepoDependencyAcceptanceScanLimit {
		rows = append(rows, repoDependencyShardPagingRow(
			fmt.Sprintf("foreign-%05d", i), foreignUnit, now.Add(time.Duration(i)*time.Nanosecond),
		))
	}
	inner := &nonAdvancingRepoDependencyIntentReader{rows: rows}
	sharded := &repoDependencyShardReader{inner: inner, shardID: shardID, shardCount: shardCount}

	_, err := sharded.ListPendingDomainIntents(context.Background(), DomainRepoDependency, 100)
	if err == nil || !strings.Contains(err.Error(), "did not advance") {
		t.Fatalf("non-advancing continuation error = %v, want explicit cursor error", err)
	}
	if inner.continuationCalls != 1 {
		t.Fatalf("continuation calls = %d, want 1 before fail-closed cursor detection", inner.continuationCalls)
	}
}

func repoDependencyUnitForShard(t *testing.T, shardID, shardCount int) string {
	t.Helper()
	for i := range 10_000 {
		unitID := fmt.Sprintf("repository:shard-paging-%d", i)
		if ifaRepoDependencyAcceptanceShard(unitID, shardCount) == shardID {
			return unitID
		}
	}
	t.Fatalf("no acceptance unit found for shard %d/%d", shardID, shardCount)
	return ""
}

func repoDependencyShardPagingRow(intentID, unitID string, createdAt time.Time) SharedProjectionIntentRow {
	return repoDependencyIntentRow(
		intentID,
		"scope-"+unitID,
		unitID,
		unitID,
		"run-"+intentID,
		"generation-target",
		createdAt,
		map[string]any{
			"repo_id":           unitID,
			"target_repo_id":    "repository:shared-target",
			"relationship_type": "DEPENDS_ON",
			"evidence_source":   crossRepoEvidenceSource,
		},
	)
}

type pagedRepoDependencyIntentReader struct {
	rows              []SharedProjectionIntentRow
	continuationCalls int
}

func (r *pagedRepoDependencyIntentReader) ListPendingDomainIntents(
	_ context.Context, _ string, limit int,
) ([]SharedProjectionIntentRow, error) {
	return truncateRowsForLimit(r.rows, limit), nil
}

func (r *pagedRepoDependencyIntentReader) ListPendingDomainIntentsAfter(
	_ context.Context,
	_ string,
	afterCreatedAt time.Time,
	afterIntentID string,
	limit int,
) ([]SharedProjectionIntentRow, error) {
	r.continuationCalls++
	rows := make([]SharedProjectionIntentRow, 0, limit)
	for _, row := range r.rows {
		if row.CreatedAt.Before(afterCreatedAt) ||
			(row.CreatedAt.Equal(afterCreatedAt) && row.IntentID <= afterIntentID) {
			continue
		}
		rows = append(rows, row)
		if len(rows) == limit {
			break
		}
	}
	return rows, nil
}

func (r *pagedRepoDependencyIntentReader) ListAcceptanceUnitDomainIntents(
	context.Context, string, string, int,
) ([]SharedProjectionIntentRow, error) {
	return nil, nil
}

func (r *pagedRepoDependencyIntentReader) MarkIntentsCompleted(context.Context, []string, time.Time) error {
	return nil
}

type nonPagedRepoDependencyIntentReader struct {
	rows []SharedProjectionIntentRow
}

func (r *nonPagedRepoDependencyIntentReader) ListPendingDomainIntents(
	_ context.Context, _ string, limit int,
) ([]SharedProjectionIntentRow, error) {
	return truncateRowsForLimit(r.rows, limit), nil
}

func (r *nonPagedRepoDependencyIntentReader) ListAcceptanceUnitDomainIntents(
	context.Context, string, string, int,
) ([]SharedProjectionIntentRow, error) {
	return nil, nil
}

func (r *nonPagedRepoDependencyIntentReader) MarkIntentsCompleted(context.Context, []string, time.Time) error {
	return nil
}

type nonAdvancingRepoDependencyIntentReader struct {
	rows              []SharedProjectionIntentRow
	continuationCalls int
}

func (r *nonAdvancingRepoDependencyIntentReader) ListPendingDomainIntents(
	_ context.Context, _ string, limit int,
) ([]SharedProjectionIntentRow, error) {
	return truncateRowsForLimit(r.rows, limit), nil
}

func (r *nonAdvancingRepoDependencyIntentReader) ListPendingDomainIntentsAfter(
	context.Context, string, time.Time, string, int,
) ([]SharedProjectionIntentRow, error) {
	r.continuationCalls++
	if r.continuationCalls > 1 {
		return nil, fmt.Errorf("continuation loop sentinel")
	}
	return r.rows, nil
}

func (r *nonAdvancingRepoDependencyIntentReader) ListAcceptanceUnitDomainIntents(
	context.Context, string, string, int,
) ([]SharedProjectionIntentRow, error) {
	return nil, nil
}

func (r *nonAdvancingRepoDependencyIntentReader) MarkIntentsCompleted(context.Context, []string, time.Time) error {
	return nil
}
