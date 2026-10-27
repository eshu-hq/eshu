// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8950 RED-arm control for the two-statement writer: the same metadata-only batch with indexed_at added to the SET list must show zero HOT updates, proving the HOT result comes from leaving indexed_at alone; enrolled in the live-postgres-readiness runner; wall time is logged, not gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_GENERATION_RETENTION_PROOF_DISPOSABLE ESHU_GENERATION_RETENTION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// hotLiveMetadataVariantValues builds the VALUES list and args for the
// metadata-only UPDATE shape from the shipped constants, so a change to the
// column width or placeholder casts breaks this control loudly instead of
// silently testing a different statement.
func hotLiveMetadataVariantValues(repoID, commit, gen string, bodies []string) (string, []any) {
	tags := contentFileTagParam(gen)
	args := make([]any, 0, len(bodies)*columnsPerMetadataOnlyUpdate)
	var values strings.Builder
	for i, body := range bodies {
		if i > 0 {
			values.WriteString(", ")
		}
		values.WriteString("(")
		for c := 0; c < columnsPerMetadataOnlyUpdate; c++ {
			if c > 0 {
				values.WriteString(", ")
			}
			fmt.Fprintf(&values, "$%d::%s", i*columnsPerMetadataOnlyUpdate+c+1, metadataOnlyValueTypes[c])
		}
		values.WriteString(")")
		args = append(
			args,
			repoID,
			fmt.Sprintf("finding_%02d.go", i),
			commit,
			tags,
			fmt.Sprintf("hot-digest-%d", i),
			len(body),
			body,
			lineCount(body),
			"go",
			nil,
			nil,
			nil,
		)
	}
	return values.String(), args
}

// hotLiveStatDeltas flushes stats and returns the content_files n_tup_upd and
// n_tup_hot_upd deltas since before. The schema name scopes the counters to
// the caller's isolated schema.
func hotLiveStatDeltas(t *testing.T, ctx context.Context, db *sql.DB, schema string, beforeUpd, beforeHot int64) (upd, hot int64) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `SELECT pg_stat_force_next_flush()`); err != nil {
		t.Fatalf("force stats flush: %v", err)
	}
	var afterUpd, afterHot int64
	if err := db.QueryRowContext(ctx, `
SELECT n_tup_upd, n_tup_hot_upd FROM pg_stat_user_tables
WHERE schemaname = $1 AND relname = 'content_files'`, schema).Scan(&afterUpd, &afterHot); err != nil {
		t.Fatalf("stat deltas: %v", err)
	}
	return afterUpd - beforeUpd, afterHot - beforeHot
}

// TestContentWriterMetadataOnlyIndexedAtMoveIsNotHOTLive is the #8950 RED-arm
// control: the same metadata-only batch with indexed_at added to the SET list
// must show zero HOT updates. It proves the HOT result comes from leaving
// indexed_at alone, not from fillfactor or body shape: moving an indexed
// (INCLUDE) column forces a new index entry, so the update can never stay on
// its page.
func TestContentWriterMetadataOnlyIndexedAtMoveIsNotHOTLive(t *testing.T) {
	ctx, database := openContentUpsertGuardDatabase(t)
	const repoID = "repo-8950-redarm"
	writer, _ := newLiveOutcomeWriter(t, database)

	bodies := hotLiveBodies(t)
	if _, err := writer.Write(ctx, hotLiveMaterialization(repoID, "gen-1", "c1", bodies)); err != nil {
		t.Fatalf("seed Write: %v", err)
	}
	prepareHotLiveTable(t, ctx, database)
	before := readContentFileVersions(t, ctx, database, repoID)

	var schema string
	if err := database.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatalf("current schema: %v", err)
	}
	if _, err := database.ExecContext(ctx, `SELECT pg_stat_force_next_flush()`); err != nil {
		t.Fatalf("force stats flush: %v", err)
	}
	var beforeUpd, beforeHot int64
	if err := database.QueryRowContext(ctx, `
SELECT n_tup_upd, n_tup_hot_upd FROM pg_stat_user_tables
WHERE schemaname = $1 AND relname = 'content_files'`, schema).Scan(&beforeUpd, &beforeHot); err != nil {
		t.Fatalf("baseline stats: %v", err)
	}

	// The shipped metadata UPDATE plus the one forbidden assignment: the
	// indexed_at move is the only difference from the HOT path.
	const shippedSET = "SET commit_sha = v.commit_sha, generation_id = v.generation_id"
	variantPrefix := strings.Replace(metadataOnlyContentFileUpdatePrefix, shippedSET,
		shippedSET+", indexed_at = clock_timestamp()", 1)
	if variantPrefix == metadataOnlyContentFileUpdatePrefix {
		t.Fatal("shipped SET list not found: the RED arm no longer differs by one assignment")
	}
	values, args := hotLiveMetadataVariantValues(repoID, "c2", "gen-2", bodies)
	beforeLSN := walLSN(t, ctx, database)
	rows, err := database.QueryContext(ctx, variantPrefix+values+metadataOnlyContentFileUpdateSuffix, args...)
	if err != nil {
		t.Fatalf("indexed_at-moving UPDATE: %v", err)
	}
	updated := 0
	for rows.Next() {
		updated++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read updated keys: %v", err)
	}
	_ = rows.Close()
	walBytes := walBytesBetween(t, ctx, database, beforeLSN, walLSN(t, ctx, database))

	if updated != hotLiveFileCount {
		t.Fatalf("variant UPDATE touched %d rows, want %d: the recheck must match the HOT path", updated, hotLiveFileCount)
	}
	upd, hot := hotLiveStatDeltas(t, ctx, database, schema, beforeUpd, beforeHot)
	t.Logf("RED arm (indexed_at in SET): upd=%d hot=%d WAL=%d bytes, stats %s", upd, hot, walBytes, hotStatLine(ctx, database))
	if upd != hotLiveFileCount {
		t.Errorf("n_tup_upd delta = %d, want %d: every row must rewrite, not skip", upd, hotLiveFileCount)
	}
	if hot != 0 {
		t.Errorf("n_tup_hot_upd delta = %d, want 0: moving indexed_at must cost every HOT update", hot)
	}
	after := readContentFileVersions(t, ctx, database, repoID)
	for path, was := range before {
		now := after[path]
		if now.xmin == was.xmin {
			t.Errorf("%s kept xmin %s: the variant must rewrite every row", path, was.xmin)
		}
		if now.indexedAt.Equal(was.indexedAt) {
			t.Errorf("%s kept indexed_at: the variant must move it", path)
		}
	}
}
