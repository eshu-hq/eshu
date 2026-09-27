// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// scaleProofEnv selects one proof of arbiter ruling arb-7127-g8 on the 1.0x
// fixture: p3 (old against new statement, equal rows), p8b (absent against
// fresh statistics, ratio) or p8c (the churned n=4 harness with each link's
// plan captured before it runs).
const scaleProofEnv = "ESHU_CHANGED_SINCE_LINK_SCALE_PROOF"

// linkStatement returns the statement a proof runs: the shipped statement,
// or the frozen pre-fix one when ESHU_CHANGED_SINCE_LINK_SCALE_STATEMENT is
// "prefix".
func linkStatement() (string, string) {
	if os.Getenv("ESHU_CHANGED_SINCE_LINK_SCALE_STATEMENT") == "prefix" {
		return "prefix", shippedIncrementalLinkSQL
	}
	return "current", linksfreshnessstore.IncrementalLinkSQL
}

// txLinkSettings opens a transaction at the link's settings.
func txLinkSettings(t *testing.T, ctx context.Context, raw *sql.DB) *sql.Tx {
	t.Helper()
	tx, err := raw.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	for _, s := range []string{
		`SET LOCAL work_mem = '256MB'`, `SET LOCAL plan_cache_mode = force_custom_plan`, `SET LOCAL statement_timeout = '120s'`,
	} {
		if _, err := tx.ExecContext(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	return tx
}

// planSummary is what a proof records about one link's plan.
type planSummary struct {
	StateRows   float64  `json:"state_rows"`
	ClassFailed []string `json:"class_failures"`
}

func explainLink(t *testing.T, ctx context.Context, tx *sql.Tx, statement string, args []any) planSummary {
	t.Helper()
	var plan string
	if err := tx.QueryRowContext(ctx, `EXPLAIN (FORMAT JSON) `+statement, args...).Scan(&plan); err != nil {
		t.Fatalf("explain: %v", err)
	}
	failures, rows := planClassFailures(t, plan, true)
	return planSummary{StateRows: rows, ClassFailed: failures}
}

func isTimeout(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "57014"
}

func rootAtF0(t *testing.T, ctx context.Context, raw *sql.DB, store postgres.SQLDB, scopes []string) {
	t.Helper()
	resetScaleScopes(t, ctx, raw, scopes)
	writer := linksfreshnessstore.NewLinkWriter(store)
	writer.Slots = 4
	for _, scopeID := range scopes {
		if res, err := writer.LinkNext(ctx, scopeID); err != nil || res.Kind != linksfreshnessstore.LinkKindRoot {
			t.Fatalf("root %s: %+v %v", scopeID, res, err)
		}
	}
}

func linkArgs(t *testing.T, ctx context.Context, raw *sql.DB, scopeID string, at time.Time) []any {
	t.Helper()
	return []any{
		scopeID, scaleGeneration(t, ctx, raw, scopeID, "F1"), scaleGeneration(t, ctx, raw, scopeID, "F0"),
		linksfreshnessstore.DigestVersion, at,
	}
}

// ledgerDigests hashes the scope's ledger rows inside tx.
func ledgerDigests(t *testing.T, ctx context.Context, tx *sql.Tx, scopeID string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for name, query := range map[string]string{
		"state": `SELECT fact_category || '|' || stable_fact_key || '|' || fact_kind || '|' || encode(state, 'hex') || '|' || COALESCE(owner_uri, '')
			FROM changed_since_key_state WHERE scope_id = $1`,
		"deltas": `SELECT fact_category || '|' || classification || '|' || stable_fact_key || '|' || COALESCE(prior_fact_kind, '') || '|' ||
			COALESCE(current_fact_kind, '') || '|' || COALESCE(encode(prior_state, 'hex'), '') || '|' || COALESCE(encode(current_state, 'hex'), '') || '|' || current_tombstoned
			FROM changed_since_link_deltas WHERE scope_id = $1`,
		"buckets": `SELECT generation_id || '|' || prior_generation_id || '|' || fact_category || '|' || classification || '|' || key_count
			FROM changed_since_link_bucket_counts WHERE scope_id = $1`,
		"links": `SELECT generation_id || '|' || prior_generation_id || '|' || link_kind || '|' || delta_rows || '|' || files_keys || '|' ||
			content_entities_keys || '|' || facts_keys || '|' || computed_at
			FROM changed_since_links WHERE scope_id = $1`,
	} {
		var digest sql.NullString
		if err := tx.QueryRowContext(ctx, `SELECT encode(sha256(string_agg(x, E'\n' ORDER BY x)::bytea), 'hex') FROM (`+query+`) AS rows(x)`,
			scopeID).Scan(&digest); err != nil {
			t.Fatalf("digest %s: %v", name, err)
		}
		out[name] = digest.String
	}
	return out
}

// TestLinkScaleProof runs one scale proof of arbiter ruling arb-7127-g8. It
// is driven by docs/internal/evidence/7127-link-writer-scale.py --proof.
func TestLinkScaleProof(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv(scaleDSNEnv))
	proof := os.Getenv(scaleProofEnv)
	if dsn == "" || proof == "" {
		t.Skipf("set %s and %s", scaleDSNEnv, scaleProofEnv)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = raw.Close() }()
	raw.SetMaxOpenConns(16)
	store := postgres.SQLDB{DB: raw}
	switch proof {
	case "p3":
		proofOldAgainstNew(t, ctx, raw, store)
	case "p8b":
		proofAbsentAgainstFresh(t, ctx, raw, store)
	case "p8c":
		proofChurnedCapture(t, ctx, raw, store)
	default:
		t.Fatalf("unknown proof %q", proof)
	}
}

// proofOldAgainstNew is P3: the pre-fix and the fixed statement produce the
// same state, link deltas, bucket counts and link rows at 1.0x.
func proofOldAgainstNew(t *testing.T, ctx context.Context, raw *sql.DB, store postgres.SQLDB) {
	target := scaleTargets[0]
	rootAtF0(t, ctx, raw, store, []string{target})
	if _, err := raw.ExecContext(ctx, `ANALYZE changed_since_key_state`); err != nil {
		t.Fatalf("analyze: %v", err)
	}
	at := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	digests := map[string]map[string]string{}
	for name, statement := range map[string]string{"prefix": shippedIncrementalLinkSQL, "current": linksfreshnessstore.IncrementalLinkSQL} {
		tx := txLinkSettings(t, ctx, raw)
		began := time.Now()
		if _, err := tx.ExecContext(ctx, statement, linkArgs(t, ctx, raw, target, at)...); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		elapsed := time.Since(began)
		digests[name] = ledgerDigests(t, ctx, tx, target)
		_ = tx.Rollback()
		emit(t, map[string]any{"event": "p3_run", "statement": name, "seconds": elapsed.Seconds(), "digests": digests[name]})
	}
	equal := true
	for key, v := range digests["prefix"] {
		if digests["current"][key] != v {
			equal = false
			t.Errorf("P3: %s differs between the pre-fix and the fixed statement", key)
		}
	}
	emit(t, map[string]any{"event": "p3_done", "equal": equal})
}

// plantAbsent removes a scope from the state statistics and restores its rows.
func plantAbsent(t *testing.T, ctx context.Context, raw *sql.DB, scopeID string) {
	t.Helper()
	for _, s := range []string{
		`CREATE TABLE p8_saved AS SELECT * FROM changed_since_key_state WHERE scope_id = $1`,
		`DELETE FROM changed_since_key_state WHERE scope_id = $1`,
		`ANALYZE changed_since_key_state`,
		`INSERT INTO changed_since_key_state SELECT * FROM p8_saved`,
		`DROP TABLE p8_saved`,
	} {
		args := []any{scopeID}
		if !strings.Contains(s, "$1") {
			args = nil
		}
		if _, err := raw.ExecContext(ctx, s, args...); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

// proofAbsentAgainstFresh is P8(b): the fixed statement at 1.0x with the
// scope absent from the statistics against itself with fresh statistics,
// five interleaved rounds, first mover alternating; gate: paired median of
// absent over fresh at most 2.0.
func proofAbsentAgainstFresh(t *testing.T, ctx context.Context, raw *sql.DB, store postgres.SQLDB) {
	target := scaleTargets[0]
	rootAtF0(t, ctx, raw, store, []string{target})
	name, statement := linkStatement()
	if _, err := raw.ExecContext(ctx, `ALTER TABLE changed_since_key_state SET (autovacuum_enabled = false)`); err != nil {
		t.Fatalf("autovacuum off: %v", err)
	}
	defer func() { _, _ = raw.ExecContext(ctx, `ALTER TABLE changed_since_key_state RESET (autovacuum_enabled)`) }()
	var ratios []float64
	for round := range 5 {
		order := []string{"absent", "fresh"}
		if round%2 == 1 {
			order = []string{"fresh", "absent"}
		}
		times := map[string]float64{}
		for _, state := range order {
			if state == "absent" {
				plantAbsent(t, ctx, raw, target)
			} else if _, err := raw.ExecContext(ctx, `ANALYZE changed_since_key_state`); err != nil {
				t.Fatalf("analyze: %v", err)
			}
			tx := txLinkSettings(t, ctx, raw)
			args := linkArgs(t, ctx, raw, target, time.Now())
			plan := explainLink(t, ctx, tx, statement, args)
			began := time.Now()
			_, err := tx.ExecContext(ctx, statement, args...)
			elapsed := time.Since(began).Seconds()
			_ = tx.Rollback()
			if err != nil && !isTimeout(err) {
				t.Fatalf("round %d %s: %v", round, state, err)
			}
			times[state] = elapsed
			emit(t, map[string]any{
				"event": "p8b_run", "statement": name, "round": round, "stats": state, "seconds": elapsed,
				"timed_out": err != nil, "plan": plan, "load1": hostLoad1(),
			})
		}
		ratios = append(ratios, times["absent"]/times["fresh"])
	}
	median := medianDuration(ratios)
	emit(t, map[string]any{"event": "p8b_done", "statement": name, "ratios": ratios, "median_ratio": median})
	if median > 2.0 {
		t.Errorf("P8(b): paired median absent/fresh = %.2f, want at most 2.0", median)
	}
}

// proofChurnedCapture is P8(c): the churned harness that produced the stall,
// n=4 per window, each window re-rooting the four scopes (autoanalyze stays
// on), and each link's plan captured by EXPLAIN in its own transaction
// before the statement runs. It runs ESHU_CHANGED_SINCE_LINK_SCALE_ROUNDS
// windows (default 30); with the pre-fix statement it stops at the first
// stall, whose plan is then on record.
func proofChurnedCapture(t *testing.T, ctx context.Context, raw *sql.DB, store postgres.SQLDB) {
	name, statement := linkStatement()
	windows, _ := strconv.Atoi(os.Getenv("ESHU_CHANGED_SINCE_LINK_SCALE_ROUNDS"))
	if windows <= 0 {
		windows = 30
	}
	timeouts, estimatedAtOne := 0, 0
	for window := range windows {
		rootAtF0(t, ctx, raw, store, scaleTargets)
		type result struct {
			plan     planSummary
			seconds  float64
			timedOut bool
		}
		results := make([]result, len(scaleTargets))
		var wg sync.WaitGroup
		for i, scopeID := range scaleTargets {
			wg.Add(1)
			go func() {
				defer wg.Done()
				tx := txLinkSettings(t, ctx, raw)
				defer func() { _ = tx.Rollback() }()
				args := linkArgs(t, ctx, raw, scopeID, time.Now())
				plan := explainLink(t, ctx, tx, statement, args)
				began := time.Now()
				_, err := tx.ExecContext(ctx, statement, args...)
				results[i] = result{plan: plan, seconds: time.Since(began).Seconds(), timedOut: isTimeout(err)}
				if err != nil && !isTimeout(err) {
					t.Errorf("window %d %s: %v", window, scopeID, err)
					return
				}
				if err == nil {
					_ = tx.Commit()
				}
			}()
		}
		wg.Wait()
		stalled := false
		for i, r := range results {
			if r.timedOut {
				timeouts++
				stalled = true
			}
			if r.plan.StateRows <= 1 {
				estimatedAtOne++
			}
			emit(t, map[string]any{
				"event": "p8c_link", "statement": name, "window": window, "scope": scaleTargets[i],
				"seconds": r.seconds, "timed_out": r.timedOut, "plan": r.plan,
			})
		}
		if stalled && name == "prefix" {
			break
		}
	}
	emit(t, map[string]any{"event": "p8c_done", "statement": name, "timeouts": timeouts, "links_estimated_at_one_row": estimatedAtOne})
	if name == "current" && timeouts > 0 {
		t.Errorf("P8(c): %d links of the fixed statement hit the statement timeout", timeouts)
	}
}
