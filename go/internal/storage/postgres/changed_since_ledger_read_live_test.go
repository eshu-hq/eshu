// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/status/changedsince"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The PR-3c live contract (#7127): with the ledger-read switch on, a served
// window returns exactly the legacy rows, and every miss returns exactly the
// legacy rows through the fallback. The tests below plant links with the
// shipped writer statements over the G1 accuracy fixture (duplicates,
// tombstones, a never-live tombstone, a kind change, reducer kinds,
// indexed_at-bearing payloads) and compare flag-on against flag-off digests.

// plantLedgerCursor sets the scope cursor the ledger read validates.
func plantLedgerCursor(t *testing.T, ctx context.Context, database *sql.DB, scopeID, state string, seq int64, digest int16) {
	t.Helper()
	var stateArg any
	if state != "" {
		stateArg = state
	}
	if _, err := database.ExecContext(ctx, `
INSERT INTO changed_since_scope_cursor (scope_id, state_generation_id, state_activation_seq, digest_version, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (scope_id) DO UPDATE SET state_generation_id = EXCLUDED.state_generation_id,
    state_activation_seq = EXCLUDED.state_activation_seq, digest_version = EXCLUDED.digest_version`,
		scopeID, stateArg, seq, digest); err != nil {
		t.Fatalf("plant cursor: %v", err)
	}
}

// linkLedgerScope seeds the G1 fixture, plants the shipped root plus two
// incrementals, points the cursor at the latest generation, and activates
// it, so flag-on reads can serve while flag-off reads answer legacy.
func linkLedgerScope(t *testing.T, ctx context.Context, database *sql.DB, scopeID string) {
	t.Helper()
	seedLinkAccuracyScope(t, ctx, database, scopeID)
	runPlantedLinks(t, ctx, database, scopeID, linksfreshnessstore.RootLinkSQL, linksfreshnessstore.IncrementalLinkSQL)
	plantLedgerCursor(t, ctx, database, scopeID, scopeID+"-g2", 2, linksfreshnessstore.DigestVersion)
	if _, err := database.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`,
		scopeID, scopeID+"-g2"); err != nil {
		t.Fatalf("activate: %v", err)
	}
}

// ledgerReadSummary runs one changed-since read with the ledger switch as
// given.
func ledgerReadSummary(t *testing.T, ctx context.Context, database *sql.DB, scopeID, sinceGen string, sampleLimit int, flag bool) changedsince.Summary {
	t.Helper()
	store := NewStatusStore(SQLDB{DB: database})
	store.LedgerReadEnabled = flag
	filter := changedsince.Filter{ScopeID: scopeID, SinceGenerationID: sinceGen, SampleLimit: sampleLimit}.Normalize()
	summary, err := store.ComputeChangedSinceDelta(ctx, filter)
	if err != nil {
		t.Fatalf("flag=%v read: %v", flag, err)
	}
	return summary
}

// ledgerSummaryDigest canonicalizes a summary the way the acceptance
// criterion means rows equal: the full category payload, samples and
// truncation flags included.
func ledgerSummaryDigest(t *testing.T, summary changedsince.Summary) string {
	t.Helper()
	raw, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// assertLedgerMatchesLegacy fails when the flag-on read differs anywhere
// from the flag-off legacy read of the same window.
func assertLedgerMatchesLegacy(t *testing.T, ctx context.Context, database *sql.DB, scopeID, sinceGen string, sampleLimit int) {
	t.Helper()
	legacy := ledgerReadSummary(t, ctx, database, scopeID, sinceGen, sampleLimit, false)
	if legacy.Unavailable {
		t.Fatalf("legacy read of %s is unavailable (%q): fixture cannot prove equality", sinceGen, legacy.UnavailableReason)
	}
	served := ledgerReadSummary(t, ctx, database, scopeID, sinceGen, sampleLimit, true)
	if legacyDigest, servedDigest := ledgerSummaryDigest(t, legacy), ledgerSummaryDigest(t, served); legacyDigest != servedDigest {
		legacyRaw, _ := json.Marshal(legacy)
		servedRaw, _ := json.Marshal(served)
		t.Fatalf("flag-on read differs from legacy for %s (limit %d):\nlegacy: %s\nserved: %s",
			sinceGen, sampleLimit, legacyRaw, servedRaw)
	}
}

func TestChangedSinceLedgerReadMatchesLegacy(t *testing.T) {
	ctx, database := openLinkAccuracyDB(t)
	const scopeID = "pr3c-live"
	linkLedgerScope(t, ctx, database, scopeID)

	for _, tc := range []struct {
		name        string
		sinceGen    int
		sampleLimit int
		wantOutcome string
	}{
		{name: "single link", sinceGen: 1, sampleLimit: 25, wantOutcome: "ledger_single"},
		{name: "single link truncated", sinceGen: 1, sampleLimit: 2, wantOutcome: "ledger_single"},
		{name: "chain of two", sinceGen: 0, sampleLimit: 25, wantOutcome: "ledger_chain"},
		{name: "chain of two truncated", sinceGen: 0, sampleLimit: 2, wantOutcome: "ledger_chain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertLedgerMatchesLegacy(t, ctx, database, scopeID, fmt.Sprintf("%s-g%d", scopeID, tc.sinceGen), tc.sampleLimit)
			// The equality above is vacuous unless the read really served
			// from the ledger, so count the outcome of one more read.
			reader := sdkmetric.NewManualReader()
			instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
			if err != nil {
				t.Fatalf("NewInstruments: %v", err)
			}
			store := NewStatusStore(SQLDB{DB: database})
			store.LedgerReadEnabled = true
			store.Instruments = instruments
			filter := changedsince.Filter{
				ScopeID:           scopeID,
				SinceGenerationID: fmt.Sprintf("%s-g%d", scopeID, tc.sinceGen),
				SampleLimit:       tc.sampleLimit,
			}.Normalize()
			if _, err := store.ComputeChangedSinceDelta(ctx, filter); err != nil {
				t.Fatalf("metered read: %v", err)
			}
			var rm metricdata.ResourceMetrics
			if err := reader.Collect(ctx, &rm); err != nil {
				t.Fatalf("collect: %v", err)
			}
			served := false
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					if m.Name != "eshu_dp_changed_since_ledger_reads_total" {
						continue
					}
					sum, ok := m.Data.(metricdata.Sum[int64])
					if !ok {
						t.Fatalf("%s is %T, want an int64 sum", m.Name, m.Data)
					}
					for _, dp := range sum.DataPoints {
						outcome, _ := dp.Attributes.Value(telemetry.MetricDimensionOutcome)
						if outcome.AsString() == tc.wantOutcome && dp.Value == 1 {
							served = true
						}
					}
				}
			}
			if !served {
				t.Fatalf("equality held but no %s was counted: the read may have fallen back", tc.wantOutcome)
			}
		})
	}
}

// TestChangedSinceLedgerReadFallbacksMatchLegacy plants one miss per
// fallback reason (plus a planted orphan proving full-key isolation) and
// requires exact legacy rows through every one of them.
func TestChangedSinceLedgerReadFallbacksMatchLegacy(t *testing.T) {
	ctx, database := openLinkAccuracyDB(t)

	newScope := func(t *testing.T, name string) string {
		t.Helper()
		scopeID := "pr3c-fallback-" + name
		linkLedgerScope(t, ctx, database, scopeID)
		return scopeID
	}
	current := func(scopeID string) string { return scopeID + "-g2" }

	t.Run("current unlinked", func(t *testing.T) {
		scopeID := "pr3c-fallback-fresh"
		seedLinkAccuracyScope(t, ctx, database, scopeID)
		if _, err := database.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`,
			scopeID, scopeID+"-g2"); err != nil {
			t.Fatalf("activate: %v", err)
		}
		assertLedgerMatchesLegacy(t, ctx, database, scopeID, scopeID+"-g1", 5)
	})

	t.Run("prior off chain", func(t *testing.T) {
		scopeID := newScope(t, "gap")
		// Keep a link into the current generation from another prior and
		// drop the true chain: the walk from g0 cannot reach g2.
		if _, err := database.ExecContext(ctx, `DELETE FROM changed_since_link_deltas
            WHERE scope_id = $1 AND generation_id = $2 AND prior_generation_id = $3`,
			scopeID, scopeID+"-g2", scopeID+"-g1"); err != nil {
			t.Fatalf("drop chain deltas: %v", err)
		}
		if _, err := database.ExecContext(ctx, `DELETE FROM changed_since_links
            WHERE scope_id = $1 AND generation_id = $2 AND prior_generation_id = $3`,
			scopeID, scopeID+"-g2", scopeID+"-g1"); err != nil {
			t.Fatalf("drop chain link: %v", err)
		}
		now := time.Now().UTC()
		if _, err := database.ExecContext(ctx, `INSERT INTO changed_since_links
            (scope_id, generation_id, prior_generation_id, link_kind, digest_version, delta_rows,
             files_keys, content_entities_keys, facts_keys, computed_at)
            VALUES ($1, $2, 'ghost-prior', 'incremental', $3, 0, 0, 0, 0, $4)`,
			scopeID, scopeID+"-g2", linksfreshnessstore.DigestVersion, now); err != nil {
			t.Fatalf("plant off-chain link: %v", err)
		}
		assertLedgerMatchesLegacy(t, ctx, database, scopeID, scopeID+"-g0", 5)
	})

	t.Run("planted orphan never leaks", func(t *testing.T) {
		scopeID := newScope(t, "orphan")
		// An orphan row naming another prior of the same generation, with
		// hostile content: a full-key read must never see it.
		now := time.Now().UTC()
		if _, err := database.ExecContext(ctx, `INSERT INTO changed_since_links
            (scope_id, generation_id, prior_generation_id, link_kind, digest_version, delta_rows,
             files_keys, content_entities_keys, facts_keys, computed_at)
            VALUES ($1, $2, 'ghost-prior', 'incremental', $3, 1, 1, 0, 0, $4)`,
			scopeID, scopeID+"-g1", linksfreshnessstore.DigestVersion, now); err != nil {
			t.Fatalf("plant orphan link: %v", err)
		}
		if _, err := database.ExecContext(ctx, `INSERT INTO changed_since_link_deltas
            (scope_id, generation_id, prior_generation_id, fact_category, classification, stable_fact_key,
             prior_fact_kind, current_fact_kind, prior_state, current_state, current_tombstoned)
            VALUES ($1, $2, 'ghost-prior', 'files', 'added', 'file:hostile.go', NULL, 'file', NULL, '\x01', FALSE)`,
			scopeID, scopeID+"-g1"); err != nil {
			t.Fatalf("plant orphan delta: %v", err)
		}
		assertLedgerMatchesLegacy(t, ctx, database, scopeID, scopeID+"-g0", 25)
		assertLedgerMatchesLegacy(t, ctx, database, scopeID, scopeID+"-g1", 25)
	})

	t.Run("cursor moved", func(t *testing.T) {
		scopeID := newScope(t, "moved")
		plantLedgerCursor(t, ctx, database, scopeID, scopeID+"-g1", 1, linksfreshnessstore.DigestVersion)
		assertLedgerMatchesLegacy(t, ctx, database, scopeID, scopeID+"-g0", 5)
		_ = current
	})

	t.Run("digest mismatch", func(t *testing.T) {
		scopeID := newScope(t, "digest")
		plantLedgerCursor(t, ctx, database, scopeID, scopeID+"-g2", 2, linksfreshnessstore.DigestVersion+1)
		assertLedgerMatchesLegacy(t, ctx, database, scopeID, scopeID+"-g0", 5)
	})

	t.Run("link digest mismatch", func(t *testing.T) {
		scopeID := newScope(t, "linkdigest")
		if _, err := database.ExecContext(ctx, `UPDATE changed_since_links SET digest_version = $3
            WHERE scope_id = $1 AND generation_id = $2`,
			scopeID, scopeID+"-g2", linksfreshnessstore.DigestVersion+1); err != nil {
			t.Fatalf("bump link digest: %v", err)
		}
		assertLedgerMatchesLegacy(t, ctx, database, scopeID, scopeID+"-g0", 5)
	})

	t.Run("same generation", func(t *testing.T) {
		scopeID := newScope(t, "same")
		assertLedgerMatchesLegacy(t, ctx, database, scopeID, current(scopeID), 5)
	})

	t.Run("chain too large", func(t *testing.T) {
		scopeID := newScope(t, "budget")
		previous := ledgerReadChainBudget
		ledgerReadChainBudget = 1
		defer func() { ledgerReadChainBudget = previous }()
		assertLedgerMatchesLegacy(t, ctx, database, scopeID, scopeID+"-g0", 5)
	})

	t.Run("ledger error", func(t *testing.T) {
		scopeID := newScope(t, "corrupt")
		// A retention prune landing between the walk and the fetch leaves a
		// link row whose deltas are gone: the completeness check falls back
		// instead of answering partial rows.
		if _, err := database.ExecContext(ctx, `DELETE FROM changed_since_link_deltas
            WHERE scope_id = $1 AND generation_id = $2`,
			scopeID, scopeID+"-g1"); err != nil {
			t.Fatalf("prune deltas: %v", err)
		}
		assertLedgerMatchesLegacy(t, ctx, database, scopeID, scopeID+"-g0", 5)
	})
}

// TestChangedSinceLedgerReadTakesNoLocks asserts the served read's pg_locks
// footprint from a second session: relation locks are AccessShare only,
// with no advisory, tuple, or stronger lock anywhere on the reading
// backend.
func TestChangedSinceLedgerReadTakesNoLocks(t *testing.T) {
	ctx, database := openLinkAccuracyDB(t)
	const scopeID = "pr3c-locks"
	linkLedgerScope(t, ctx, database, scopeID)

	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	var pid int
	if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatalf("pid: %v", err)
	}
	store := NewStatusStore(&countingTxQueryer{tx: tx})
	store.LedgerReadEnabled = true
	filter := changedsince.Filter{ScopeID: scopeID, SinceGenerationID: scopeID + "-g0", SampleLimit: 5}.Normalize()
	if _, err := store.ComputeChangedSinceDelta(ctx, filter); err != nil {
		t.Fatalf("read: %v", err)
	}
	rows, err := database.QueryContext(ctx, `
SELECT locktype, mode FROM pg_locks WHERE pid = $1 AND locktype IN ('relation', 'advisory', 'tuple')`, pid)
	if err != nil {
		t.Fatalf("pg_locks: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var violations []string
	for rows.Next() {
		var locktype, mode string
		if err := rows.Scan(&locktype, &mode); err != nil {
			t.Fatalf("scan lock: %v", err)
		}
		if locktype == "relation" && mode == "AccessShareLock" {
			continue
		}
		violations = append(violations, locktype+"/"+mode)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("locks: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("reader holds non-read locks: %v", strings.Join(violations, ", "))
	}
}

// TestChangedSinceLedgerReadUnderWriterInterleave links new generations
// with the real LinkWriter while reads run, and requires every read,
// served or fallen back, to equal the legacy rows of the fixed endpoints.
func TestChangedSinceLedgerReadUnderWriterInterleave(t *testing.T) {
	ctx, database := openLinkAccuracyDB(t)
	const scopeID = "pr3c-race"
	seedLinkAccuracyScope(t, ctx, database, scopeID)
	if _, err := database.ExecContext(ctx, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`,
		scopeID, scopeID+"-g2"); err != nil {
		t.Fatalf("activate: %v", err)
	}
	for gi := 0; gi <= 2; gi++ {
		if _, err := database.ExecContext(ctx, `INSERT INTO changed_since_activations
            (scope_id, generation_id, prior_generation_id, source, activated_at) VALUES ($1, $2, NULL, 'backfill', now())`,
			scopeID, fmt.Sprintf("%s-g%d", scopeID, gi)); err != nil {
			t.Fatalf("journal: %v", err)
		}
	}
	control := ledgerReadSummary(t, ctx, database, scopeID, scopeID+"-g0", 5, false)
	controlDigest := ledgerSummaryDigest(t, control)

	writer := linksfreshnessstore.NewLinkWriter(SQLDB{DB: database})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for gi := 0; gi <= 2; gi++ {
			if _, err := writer.LinkNext(ctx, scopeID); err != nil {
				return
			}
		}
	}()
	var mu sync.Mutex
	var mismatches int
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				summary := ledgerReadSummary(t, ctx, database, scopeID, scopeID+"-g0", 5, true)
				if got := ledgerSummaryDigest(t, summary); got != controlDigest {
					mu.Lock()
					mismatches++
					mu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	if mismatches > 0 {
		t.Fatalf("%d racing reads differed from the legacy rows", mismatches)
	}
}

// TestChangedSinceLedgerReadUnderRetentionInterleave prunes a middle link's
// deltas while reads run: every read must equal the legacy rows, served
// before the prune or fallen back after it, never partial.
func TestChangedSinceLedgerReadUnderRetentionInterleave(t *testing.T) {
	ctx, database := openLinkAccuracyDB(t)
	const scopeID = "pr3c-retention"
	linkLedgerScope(t, ctx, database, scopeID)
	control := ledgerReadSummary(t, ctx, database, scopeID, scopeID+"-g0", 5, false)
	controlDigest := ledgerSummaryDigest(t, control)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = database.ExecContext(ctx, `DELETE FROM changed_since_link_deltas
            WHERE scope_id = $1 AND generation_id = $2`, scopeID, scopeID+"-g1")
	}()
	var mu sync.Mutex
	var mismatches int
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				summary := ledgerReadSummary(t, ctx, database, scopeID, scopeID+"-g0", 5, true)
				if got := ledgerSummaryDigest(t, summary); got != controlDigest {
					mu.Lock()
					mismatches++
					mu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	if mismatches > 0 {
		t.Fatalf("%d racing reads differed from the legacy rows", mismatches)
	}
}
