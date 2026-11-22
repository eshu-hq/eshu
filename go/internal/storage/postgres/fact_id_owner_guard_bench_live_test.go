// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11203 before/after cost proof for the fact_id owner guard on disposable PostgreSQL: EXPLAIN (ANALYZE, BUFFERS) of a 500-row conflicting batch with and without the owner legs (the pre-change statement is derived from the shipped constant) and a 15-run paired median-and-spread table over four 500-row mixes (all new, all changed, identical replay, one foreign row); wall time logged not gated
//eshu:readiness:env ESHU_POSTGRES_DSN

// Cost proof for the issue #11203 owner guard (factOwnerGuardClause and the
// owner columns of selectUnchangedFactIDs). The "pre" upsert statement is
// derived from the shipped upsertFactBatchSuffix by removing the clause; the
// "pre" classification query is the frozen pre-#11203 discriminator below (a
// measurement-only copy, like factGuardUnguardedSuffixForBench). Live-Postgres
// only; skipped without ESHU_POSTGRES_DSN.

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ownerGuardPreSuffix is the shipped suffix without the #11203 clause.
func ownerGuardPreSuffix() string {
	return strings.Replace(upsertFactBatchSuffixReturningFactID, factOwnerGuardClause, "", 1)
}

// ownerGuardPreDiscriminator is the pre-#11203 selectUnchangedFactIDs WHERE
// shape: join on fact_id alone, 12 guarded columns, no owner columns. It is
// frozen for measurement only and must never ship.
const ownerGuardPreDiscriminator = `SELECT v.fact_id
FROM (VALUES %s) AS v(
    fact_id, fact_kind, stable_fact_key, schema_version, collector_kind,
    fencing_token, source_confidence, source_system, source_fact_key,
    source_uri, source_record_id, is_tombstone, payload
)
JOIN fact_records AS f ON f.fact_id = v.fact_id
WHERE f.fact_kind IS NOT DISTINCT FROM v.fact_kind
  AND f.stable_fact_key IS NOT DISTINCT FROM v.stable_fact_key
  AND f.schema_version IS NOT DISTINCT FROM v.schema_version
  AND f.collector_kind IS NOT DISTINCT FROM v.collector_kind
  AND f.fencing_token IS NOT DISTINCT FROM v.fencing_token
  AND f.source_confidence IS NOT DISTINCT FROM v.source_confidence
  AND f.source_system IS NOT DISTINCT FROM v.source_system
  AND f.source_fact_key IS NOT DISTINCT FROM v.source_fact_key
  AND f.source_uri IS NOT DISTINCT FROM v.source_uri
  AND f.source_record_id IS NOT DISTINCT FROM v.source_record_id
  AND f.is_tombstone IS NOT DISTINCT FROM v.is_tombstone
  AND f.payload IS NOT DISTINCT FROM v.payload`

// ownerGuardPreClassified is the pre-#11203 upsertFactBatchReturningAccepted:
// the unowned upsert, then the unowned discriminator for RETURNING-absent ids.
func ownerGuardPreClassified(ctx context.Context, store SQLDB, batch []facts.Envelope) (int, error) {
	query, args, err := buildUpsertFactBatchQuery(ownerGuardPreSuffix(), batch)
	if err != nil {
		return 0, err
	}
	rows, err := store.QueryContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	accepted := map[string]struct{}{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
		accepted[id] = struct{}{}
	}
	_ = rows.Close()
	if len(accepted) == len(batch) {
		return len(accepted), nil
	}
	var values strings.Builder
	var dargs []any
	n := 0
	for _, e := range batch {
		if _, ok := accepted[e.FactID]; ok {
			continue
		}
		g, err := guardedFactValuesFor(e)
		if err != nil {
			return 0, err
		}
		if n > 0 {
			values.WriteString(", ")
		}
		o := n * 13
		fmt.Fprintf(&values, "($%d::text, $%d::text, $%d::text, $%d::text, $%d::text, $%d::bigint, $%d::text, $%d::text, $%d::text, $%d::text, $%d::text, $%d::boolean, $%d::jsonb)",
			o+1, o+2, o+3, o+4, o+5, o+6, o+7, o+8, o+9, o+10, o+11, o+12, o+13)
		dargs = append(dargs, e.FactID, g.factKind, g.stableFactKey, g.schemaVersion, g.collectorKind, g.fencingToken,
			g.sourceConfidence, g.sourceSystem, g.sourceFactKey, g.sourceURI, g.sourceRecordID, g.isTombstone, g.payloadJSON)
		n++
	}
	res, err := store.QueryContext(ctx, fmt.Sprintf(ownerGuardPreDiscriminator, values.String()), dargs...)
	if err != nil {
		return 0, err
	}
	defer func() { _ = res.Close() }()
	for res.Next() {
		var id string
		if err := res.Scan(&id); err != nil {
			return 0, err
		}
		accepted[id] = struct{}{}
	}
	return len(accepted), res.Err()
}

func ownerGuardBatch(prefix string, n int, scopeID, generationID string, at time.Time, tag string) []facts.Envelope {
	out := make([]facts.Envelope, 0, n)
	for j := 0; j < n; j++ {
		e := unchangedSkipEnvelope(prefix+factIDForCounter(j), scopeID, generationID, 10, at)
		e.StableFactKey = "repository:bench"
		e.Payload = map[string]any{"version": tag}
		out = append(out, e)
	}
	return out
}

// TestFactOwnerGuardExplainLive logs EXPLAIN (ANALYZE, BUFFERS) for a changed
// 500-row conflicting batch under the pre and post statements, each in
// BEGIN/ROLLBACK so the table is identical for both, alternating so cache state
// is shared.
func TestFactOwnerGuardExplainLive(t *testing.T) {
	dsn := unchangedSkipProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #11203 cost proof")
	}
	ctx := context.Background()
	db := openUnchangedSkipSchema(t, ctx, dsn)
	store := SQLDB{DB: db}
	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	seedUnchangedSkipScope(t, ctx, db, "scope-x", "gen-x", now)
	const rows = 500
	if err := upsertFacts(ctx, store, ownerGuardBatch("", rows, "scope-x", "gen-x", now, "seed")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	type explainShape struct{ name, suffix string }
	pre := explainShape{"pre", ownerGuardPreSuffix()}
	post := explainShape{"post", upsertFactBatchSuffixReturningFactID}
	for _, shape := range []explainShape{pre, post, pre, post} {
		query, args, err := buildUpsertFactBatchQuery(shape.suffix, ownerGuardBatch("", rows, "scope-x", "gen-x", now.Add(time.Hour), "changed"))
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		res, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+query, args...)
		if err != nil {
			_ = tx.Rollback()
			t.Fatalf("explain %s: %v", shape.name, err)
		}
		var plan []string
		for res.Next() {
			var line string
			if err := res.Scan(&line); err != nil {
				t.Fatalf("scan: %v", err)
			}
			plan = append(plan, line)
		}
		_ = res.Close()
		_ = tx.Rollback()
		t.Logf("PLAN %s:\n%s", shape.name, strings.Join(plan, "\n"))
	}
}

// ownerGuardMedian returns the median of values.
func ownerGuardMedian(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}
	return sorted[mid]
}

// TestFactOwnerGuardCostMixesLive prices the #11203 change on four 500-row
// mixes through the full classification path (statement plus, when rows are
// absent from RETURNING, the discriminator): all new ids, all conflicting rows
// with changed content, an all-identical replay, and a replay with one
// foreign-owned row in 500. Pre and post alternate within every run (order
// flipped each run) so host load lands on both. It logs exact seconds: the
// median and the min-max spread per shape and the post/pre ratio of medians,
// with the ruling's bar (median within 3% or inside the run-to-run spread).
// One foreign row makes the post path return the typed error, which is the
// measured outcome; the pre path admits it (the defect being fixed).
func TestFactOwnerGuardCostMixesLive(t *testing.T) {
	dsn := unchangedSkipProofDSN()
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_DSN to run the #11203 cost proof")
	}
	ctx := context.Background()
	db := openUnchangedSkipSchema(t, ctx, dsn)
	store := SQLDB{DB: db}
	now := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	seedUnchangedSkipScope(t, ctx, db, "scope-mix", "gen-mix", now)
	seedUnchangedSkipScope(t, ctx, db, "scope-other", "gen-other", now)
	const rows = 500
	const runs = 15
	const warmups = 2
	if err := upsertFacts(ctx, store, ownerGuardBatch("own-", rows, "scope-mix", "gen-mix", now, "seed")); err != nil {
		t.Fatalf("seed owner rows: %v", err)
	}
	foreign := ownerGuardBatch("own-", 1, "scope-other", "gen-other", now, "seed")[0]

	post := func(batch []facts.Envelope) error {
		_, _, err := upsertFactBatchClassified(ctx, store, batch)
		var conflict *FactIDOwnerConflictError
		if err != nil && !errors.As(err, &conflict) {
			return err
		}
		return nil
	}
	pre := func(batch []facts.Envelope) error {
		_, err := ownerGuardPreClassified(ctx, store, batch)
		return err
	}
	mixes := []struct {
		name  string
		build func(shape string, run int) []facts.Envelope
	}{
		{"all new", func(shape string, run int) []facts.Envelope {
			return ownerGuardBatch(fmt.Sprintf("new-%s-%d-", shape, run), rows, "scope-mix", "gen-mix", now, "new")
		}},
		{"all conflicting, changed", func(shape string, run int) []facts.Envelope {
			return ownerGuardBatch("own-", rows, "scope-mix", "gen-mix", now.Add(time.Duration(run)*time.Second), fmt.Sprintf("%s-%d", shape, run))
		}},
		{"all identical replay", func(string, int) []facts.Envelope {
			return ownerGuardBatch("own-", rows, "scope-mix", "gen-mix", now, "seed")
		}},
		{"identical replay, 1 foreign in 500", func(string, int) []facts.Envelope {
			batch := ownerGuardBatch("own-", rows, "scope-mix", "gen-mix", now, "seed")
			batch[rows/2] = foreign
			return batch
		}},
	}
	for _, mix := range mixes {
		var preSec, postSec []float64
		for run := -warmups; run < runs; run++ {
			order := []string{"pre", "post"}
			if run%2 != 0 {
				order = []string{"post", "pre"}
			}
			for _, shape := range order {
				batch := mix.build(shape, run+warmups)
				start := time.Now()
				var err error
				if shape == "pre" {
					err = pre(batch)
				} else {
					err = post(batch)
				}
				took := time.Since(start).Seconds()
				if err != nil {
					t.Fatalf("%s %s run %d: %v", mix.name, shape, run, err)
				}
				if run < 0 {
					continue // warm-up: cache and plan state, not recorded
				}
				if shape == "pre" {
					preSec = append(preSec, took)
				} else {
					postSec = append(postSec, took)
				}
			}
		}
		sort.Float64s(preSec)
		sort.Float64s(postSec)
		mp, mq := ownerGuardMedian(preSec), ownerGuardMedian(postSec)
		ratio := mq / mp
		inSpread := mq >= preSec[0] && mq <= preSec[len(preSec)-1]
		t.Logf("MIX %-36s pre median=%.6fs [%.6f..%.6f]  post median=%.6fs [%.6f..%.6f]  post/pre=%.3f  within 3%%=%t  post median inside pre run spread=%t",
			mix.name, mp, preSec[0], preSec[len(preSec)-1], mq, postSec[0], postSec[len(postSec)-1], ratio, ratio <= 1.03 && ratio >= 0.97, inSpread)
	}
}
