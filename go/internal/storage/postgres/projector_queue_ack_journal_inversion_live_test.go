// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestProjectorAckJournalInversion is the seeded RED/GREEN pair of the gate
// (#7127 PR-3b, arbiter ruling of 2026-10-15). The scope's active generation
// g0 was activated with the journal off, so it has no row. An Ack of g1 is
// paused after its journal insert, and a journal pass runs:
//
//   - ungated (the shipped statement with its gate removed): the pass
//     journals g0 with a higher activation_seq than g1 and commits first, so
//     the link writer ends on the superseded g0 while g1 is active. The test
//     requires the inversion, which proves it can see one.
//   - gated (the shipped statement): the pass misses the key and skips; g1
//     links, and the state is the active generation.
//
// "backfill" journals g0 through the backfill (the scope has no row);
// "sweeper" through the sweeper (an older generation is journaled already).
func TestProjectorAckJournalInversion(t *testing.T) {
	d := openAckJournalDB(t, 8)
	ctx := context.Background()
	journal := linksfreshnessstore.NewJournalStore(SQLDB{DB: d})
	for _, variant := range []string{"backfill", "sweeper"} {
		for _, shape := range []struct {
			name      string
			query     string
			wantInver bool
		}{
			{name: "ungated", query: ajUngatedActivateQuery(t), wantInver: true},
			{name: "gated", query: "", wantInver: false},
		} {
			t.Run(variant+"/"+shape.name, func(t *testing.T) {
				scopeID := "inv-" + variant + "-" + shape.name
				ajSeedScope(t, d, scopeID)
				if variant == "sweeper" {
					ajExec(t, d, `INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, is_delta, observed_at,
                        ingested_at, status, activated_at, superseded_at)
                        VALUES ($1, $2, 'snapshot', FALSE, now(), now(), 'superseded', now() - interval '2 hours', now() - interval '1 hour')`,
						scopeID+"-gm", scopeID)
					ajExec(t, d, `INSERT INTO changed_since_activations (scope_id, generation_id, prior_generation_id, source, activated_at)
                        VALUES ($1, $2, NULL, 'sweeper', now() - interval '2 hours')`, scopeID, scopeID+"-gm")
					// Link gm without a pass, so g0 stays unjournaled.
					ajDrainLinks(t, d, scopeID)
				}
				work := ajPending(t, d, scopeID, scopeID+"-g1", 1)
				_, release, done := ajPausedAck(t, d, work, shape.query)
				result, err := journal.Journal(ctx, 100)
				release()
				if ackErr := <-done; ackErr != nil {
					t.Fatalf("Ack: %v", ackErr)
				}
				if err != nil {
					t.Fatalf("journal pass: %v", err)
				}
				if !shape.wantInver && !result.Skipped {
					t.Fatalf("journal pass ran while the Ack held the gate: %+v", result)
				}
				// A later pass after the Ack committed, as the runner would run.
				if _, err := journal.Journal(ctx, 100); err != nil {
					t.Fatalf("journal pass after commit: %v", err)
				}
				ajDrainLinks(t, d, scopeID)
				seqG0, seqG1 := ajSeq(t, d, scopeID, scopeID+"-g0"), ajSeq(t, d, scopeID, scopeID+"-g1")
				state := ajLinkState(t, d, scopeID)
				inverted := seqG0 > seqG1 && seqG1 > 0
				t.Logf("%s %s: pass %+v; seq g0=%d g1=%d; link state %s (active %s-g1)",
					variant, shape.name, result, seqG0, seqG1, state, scopeID)
				if shape.wantInver {
					if !inverted || state == scopeID+"-g1" {
						t.Fatalf("the ungated statement did not plant the inversion (seq g0=%d g1=%d, state %s): the proof has no teeth",
							seqG0, seqG1, state)
					}
					return
				}
				if inverted || state != scopeID+"-g1" {
					t.Fatalf("INVERSION: seq g0=%d g1=%d, link state %s, active %s-g1", seqG0, seqG1, state, scopeID)
				}
			})
		}
	}
}

// TestProjectorAckJournalGateHeldByPass proves the other direction: while a
// journal pass holds its key, a flag-on Ack activates its generation, writes
// no journal row, counts one gate_skipped, and does not wait (it returns
// while the holder still holds the key; a wait would never return). The next pass's sweeper journals the
// generation, and it links as the active state.
func TestProjectorAckJournalGateHeldByPass(t *testing.T) {
	d := openAckJournalDB(t, 8)
	ctx := context.Background()
	ajSeedScope(t, d, "held")
	work := ajPending(t, d, "held", "held-g1", 1)
	holder, err := d.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin holder: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	var locked bool
	if err := holder.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock($1::integer, $2::integer)`,
		linksfreshnessstore.SlotLockClass, 0).Scan(&locked); err != nil || !locked {
		t.Fatalf("hold the journal key: %v %v", locked, err)
	}
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	q := ajQueue(SQLDB{DB: d}, true)
	q.Instruments = instruments
	if err := q.Ack(ctx, work, runtime.Result{}); err != nil {
		t.Fatalf("Ack with the journal key held: %v", err)
	}
	if counts := ajJournalCounts(t, reader); len(counts) != 1 || counts[ackJournalOutcomeGateSkipped] != 1 {
		t.Fatalf("journal outcome counts = %v, want one gate_skipped", counts)
	}
	if seq := ajSeq(t, d, "held", "held-g1"); seq != -1 {
		t.Fatalf("Ack wrote journal row seq %d while the pass held the key", seq)
	}
	if n := ajCount(t, d, `SELECT count(*) FROM scope_generations WHERE generation_id = 'held-g1' AND status = 'active'`); n != 1 {
		t.Fatal("Ack did not activate the generation")
	}
	_ = holder.Rollback()

	result, err := linksfreshnessstore.NewJournalStore(SQLDB{DB: d}).Journal(ctx, 100)
	if err != nil || result.Skipped {
		t.Fatalf("journal pass after release = %+v, %v", result, err)
	}
	if ajSeq(t, d, "held", "held-g1") <= 0 {
		t.Fatal("the next pass did not journal the generation the Ack skipped")
	}
	ajDrainLinks(t, d, "held")
	if state := ajLinkState(t, d, "held"); state != "held-g1" {
		t.Fatalf("link state %s, want the active held-g1", state)
	}
	t.Logf("gate skipped: no row, no wait; the pass journaled held-g1 (%+v)", result)
}
