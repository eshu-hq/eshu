// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #7127 PR-3b deterministic cost gate: one backend, journal off and on over 4 rounds of 200 Acks; statements per Ack equal, WAL records per Ack at most +6 and buffer hits at most +40 from pg_stat_get_backend_wal and pg_stat_get_backend_io (pg_stat_statements also gated when preloaded), with a seeded RED that writes three extra rows; disposable database from ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN (env Skip); PostgreSQL 18 required; about 10 s locally; enrolled in the live-postgres-readiness runner; wall ratio logged with mean and SD, never gated
//eshu:readiness:env ESHU_AWS_DRIFT_ARN_PROOF_DISPOSABLE ESHU_AWS_DRIFT_ARN_PROOF_DSN ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE ESHU_DEFERRED_PARTITION_PROOF_DSN ESHU_PROJECTOR_CLAIM_DEADLOCK_PROOF_DSN ESHU_REDUCER_FAIRNESS_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
)

// Bounds of the deterministic cost gate (#7127 PR-3b, timing-proof rules,
// rule 2): the journal adds one row, one index entry pair and one sequence
// step per Ack. The shim measured +5.1 WAL records and +7 to +38 shared
// buffer hits per Ack through pg_stat_statements; wall time is reported,
// never gated.
const (
	ajCostScopes          = 200
	ajCostRounds          = 4
	ajCostMaxExtraWAL     = 6.0
	ajCostMaxExtraHits    = 40.0
	ajCostStatementsOnAck = 9
)

// ajCostSample is one measured batch of Acks.
type ajCostSample struct {
	client, pssCalls, pssHits, pssWAL, backendWAL, backendHits, wallMS float64
}

// ajBackendCounters reads the WAL records and buffer hits PostgreSQL 18
// counts for one backend.
func ajBackendCounters(t *testing.T, reader *sql.DB, pid int) (float64, float64) {
	t.Helper()
	var wal, hits float64
	if err := reader.QueryRow(`SELECT (SELECT wal_records FROM pg_stat_get_backend_wal($1)),
            (SELECT COALESCE(sum(hits), 0) FROM pg_stat_get_backend_io($1))`, pid).Scan(&wal, &hits); err != nil {
		t.Fatalf("backend stats: %v", err)
	}
	return wal, hits
}

// TestProjectorAckJournalCost is the deterministic cost gate of #7127 PR-3b.
// One backend runs every Ack, alternating journal off and on over rounds of
// 200 Acks. Gated per Ack, on against off: the statements the Ack sends are
// equal (and the pg_stat_statements calls, when the server preloads it); WAL
// records grow by at most 6; shared buffer hits by at most 40. WAL and hits
// come from pg_stat_get_backend_wal and pg_stat_get_backend_io, which count
// only that backend, so other sessions cannot move them. The wall ratio is
// logged with its mean and standard deviation and is never gated.
func TestProjectorAckJournalCost(t *testing.T) {
	reader, dsn := openAckJournalDBWithDSN(t, 4)
	ctx := context.Background()
	ackPool, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Ack pool: %v", err)
	}
	ackPool.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = ackPool.Close() })
	var pid int
	if err := ackPool.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("Ack backend: %v", err)
	}
	pss := false
	if _, err := reader.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`); err == nil {
		_, err := reader.ExecContext(ctx, `SELECT 1 FROM pg_stat_statements LIMIT 1`)
		pss = err == nil
	}
	for s := range ajCostScopes {
		ajSeedScope(t, reader, fmt.Sprintf("k%03d", s))
	}
	flush := func() {
		if _, err := ackPool.ExecContext(ctx, `SELECT pg_stat_force_next_flush()`); err != nil {
			t.Fatalf("flush: %v", err)
		}
	}
	batch := func(label string, journal bool, query string, scopes int) ajCostSample {
		for s := range scopes {
			scopeID := fmt.Sprintf("k%03d", s)
			generationID := fmt.Sprintf("%s-%s", scopeID, label)
			ajExec(t, reader, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at,
                ingested_at, status) VALUES ($1, $2, 'snapshot', FALSE, now(), now(), 'pending')`, generationID, scopeID)
			ajRunning(t, reader, scopeID, generationID)
		}
		recorder := newAJHookDB(ackPool, nil)
		queue := ajQueue(recorder, journal)
		queue.ackJournalActivateQuery = query
		if pss {
			ajExec(t, reader, `SELECT pg_stat_statements_reset()`)
		}
		flush()
		wal0, hits0 := ajBackendCounters(t, reader, pid)
		started := time.Now()
		for s := range scopes {
			scopeID := fmt.Sprintf("k%03d", s)
			if err := queue.Ack(ctx, ajWork(scopeID, fmt.Sprintf("%s-%s", scopeID, label)), runtime.Result{}); err != nil {
				t.Fatalf("Ack %s (journal %v): %v", label, journal, err)
			}
		}
		wall := time.Since(started)
		flush()
		wal1, hits1 := ajBackendCounters(t, reader, pid)
		n := float64(scopes)
		sample := ajCostSample{
			client:      float64(len(recorder.statements())) / n,
			backendWAL:  (wal1 - wal0) / n,
			backendHits: (hits1 - hits0) / n,
			wallMS:      float64(wall.Microseconds()) / 1000 / n,
		}
		if pss {
			if err := reader.QueryRow(`SELECT COALESCE(sum(calls), 0), COALESCE(sum(shared_blks_hit), 0), COALESCE(sum(wal_records), 0)
                FROM pg_stat_statements
                WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
                  AND query NOT LIKE '%pg_stat%' AND query NOT LIKE '%pg_backend_pid%'`).Scan(
				&sample.pssCalls, &sample.pssHits, &sample.pssWAL); err != nil {
				t.Fatalf("pg_stat_statements: %v", err)
			}
			sample.pssCalls /= n
			sample.pssHits /= n
			sample.pssWAL /= n
		}
		return sample
	}
	loadavg := func() string { raw, _ := os.ReadFile("/proc/loadavg"); return strings.TrimSpace(string(raw)) }
	t.Logf("load at start: %s; pg_stat_statements preloaded: %v", loadavg(), pss)
	batch("warm-off", false, "", 20)
	batch("warm-on", true, "", 20)
	var off, on []ajCostSample
	for r := range ajCostRounds {
		order := []bool{false, true}
		if r%2 == 1 {
			order = []bool{true, false}
		}
		for _, journal := range order {
			sample := batch(fmt.Sprintf("r%d-%v", r, journal), journal, "", ajCostScopes)
			if journal {
				on = append(on, sample)
			} else {
				off = append(off, sample)
			}
		}
	}
	t.Logf("load at end: %s", loadavg())
	mean := func(samples []ajCostSample, field func(ajCostSample) float64) float64 {
		var sum float64
		for _, s := range samples {
			sum += field(s)
		}
		return sum / float64(len(samples))
	}
	var ratios []float64
	for i := range on {
		ratios = append(ratios, on[i].wallMS/off[i].wallMS)
	}
	ratioMean := 0.0
	for _, r := range ratios {
		ratioMean += r / float64(len(ratios))
	}
	ratioSD := 0.0
	for _, r := range ratios {
		ratioSD += (r - ratioMean) * (r - ratioMean) / float64(len(ratios))
	}
	ratioSD = math.Sqrt(ratioSD)
	field := map[string]func(ajCostSample) float64{
		"client statements": func(s ajCostSample) float64 { return s.client },
		"backend WAL":       func(s ajCostSample) float64 { return s.backendWAL },
		"backend hits":      func(s ajCostSample) float64 { return s.backendHits },
		"pss statements":    func(s ajCostSample) float64 { return s.pssCalls },
		"pss WAL":           func(s ajCostSample) float64 { return s.pssWAL },
		"pss hits":          func(s ajCostSample) float64 { return s.pssHits },
		"wall ms":           func(s ajCostSample) float64 { return s.wallMS },
	}
	for _, name := range []string{"client statements", "pss statements", "backend WAL", "pss WAL", "backend hits", "pss hits", "wall ms"} {
		t.Logf("per Ack %-17s off=%.2f on=%.2f (on-off %+.2f)", name, mean(off, field[name]), mean(on, field[name]),
			mean(on, field[name])-mean(off, field[name]))
	}
	t.Logf("wall ratio on/off per round %v: mean %.3f SD %.3f (reported, not gated)", ratios, ratioMean, ratioSD)

	if violations := ajCostViolations(off, on, pss); len(violations) > 0 {
		t.Fatalf("cost gate: %v", violations)
	}
	// Seeded RED: a planted journal statement that writes three more rows
	// per Ack must fail the same verdict.
	planted := []ajCostSample{batch("planted", true, ajPlantedHeavyJournalQuery(t), ajCostScopes)}
	if violations := ajCostViolations(off[:1], planted, pss); len(violations) == 0 {
		t.Fatalf("cost gate passed a planted Ack that writes three extra rows (WAL %.2f vs %.2f): the gate has no teeth",
			planted[0].backendWAL, off[0].backendWAL)
	} else {
		t.Logf("seeded RED caught: %v", violations)
	}
}

// ajCostViolations is the gate's verdict on per-Ack means, on against off.
func ajCostViolations(off, on []ajCostSample, pss bool) []string {
	mean := func(samples []ajCostSample, field func(ajCostSample) float64) float64 {
		var sum float64
		for _, s := range samples {
			sum += field(s)
		}
		return sum / float64(len(samples))
	}
	var violations []string
	for _, samples := range [][]ajCostSample{off, on} {
		for _, s := range samples {
			if s.client != ajCostStatementsOnAck {
				violations = append(violations, fmt.Sprintf("statements per Ack %.2f, want %d", s.client, ajCostStatementsOnAck))
			}
		}
	}
	type bound struct {
		name  string
		field func(ajCostSample) float64
		max   float64
	}
	bounds := []bound{
		{"backend WAL records", func(s ajCostSample) float64 { return s.backendWAL }, ajCostMaxExtraWAL},
		{"backend buffer hits", func(s ajCostSample) float64 { return s.backendHits }, ajCostMaxExtraHits},
	}
	if pss {
		if a, b := mean(off, func(s ajCostSample) float64 { return s.pssCalls }), mean(on, func(s ajCostSample) float64 { return s.pssCalls }); a != b {
			violations = append(violations, fmt.Sprintf("pg_stat_statements calls per Ack off=%.2f on=%.2f", a, b))
		}
		bounds = append(bounds,
			bound{"pg_stat_statements WAL records", func(s ajCostSample) float64 { return s.pssWAL }, ajCostMaxExtraWAL},
			bound{"pg_stat_statements buffer hits", func(s ajCostSample) float64 { return s.pssHits }, ajCostMaxExtraHits})
	}
	for _, b := range bounds {
		if extra := mean(on, b.field) - mean(off, b.field); extra > b.max {
			violations = append(violations, fmt.Sprintf("%s per Ack grew by %.2f, bound %.0f", b.name, extra, b.max))
		}
	}
	return violations
}

// ajPlantedHeavyJournalQuery derives a heavier journal statement from the
// shipped one: it also inserts three throwaway activation rows per Ack.
func ajPlantedHeavyJournalQuery(t *testing.T) string {
	t.Helper()
	marker := "    RETURNING 1\n)\n"
	if strings.Count(activateAndJournalProjectorGenerationQuery, marker) != 1 {
		t.Fatalf("shipped statement lost %q", marker)
	}
	return strings.Replace(activateAndJournalProjectorGenerationQuery, marker, marker[:len(marker)-1]+`, planted AS (
    INSERT INTO changed_since_activations (scope_id, generation_id, prior_generation_id, source, activated_at)
    SELECT activated.scope_id, activated.generation_id || '-planted-' || n, NULL, 'planted', activated.activated_at
    FROM activated, generate_series(1, 3) AS n
    RETURNING 1
)
`, 1)
}
