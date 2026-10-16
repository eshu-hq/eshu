// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/projector/failure"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// ajAuditSQL records every journal row at its transaction's commit: a
// deferred constraint trigger fires in commit processing, after every lock
// that orders the rows is held, so commit_order is the commit order of the
// rows that matter here (Acks of one scope hold its scope row until commit;
// the gate keeps an Ack and a pass apart). A row inserted later by seq but
// committed earlier shows as commit_order < the lag over seq.
const ajAuditSQL = `
CREATE SEQUENCE aj_commit_order;
CREATE TABLE aj_audit (seq bigint, scope_id text, generation_id text, source text, commit_order bigint);
CREATE FUNCTION aj_audit_row() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    INSERT INTO aj_audit VALUES (NEW.activation_seq, NEW.scope_id, NEW.generation_id, NEW.source, nextval('aj_commit_order'));
    RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER aj_audit AFTER INSERT ON changed_since_activations
    DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION aj_audit_row();
`

// ajBackground counts what the journal, link and retention loops observed.
type ajBackground struct {
	journalRuns, journalSkips, links, linkRetries, retentionRuns, pruned atomic.Int64
	mu                                                                   sync.Mutex
	errs                                                                 []string
}

func (b *ajBackground) fail(msg string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.errs) < 10 {
		b.errs = append(b.errs, msg)
	}
}

// ajRunBackground runs one journal loop, four link writers and one
// retention loop until stop closes.
func ajRunBackground(d *sql.DB, scopes int, stop <-chan struct{}, wg *sync.WaitGroup) *ajBackground {
	ctx := context.Background()
	b := &ajBackground{}
	journal := linksfreshnessstore.NewJournalStore(SQLDB{DB: d})
	loop := func(pause time.Duration, step func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				step()
				time.Sleep(pause)
			}
		}()
	}
	loop(2*time.Millisecond, func() {
		result, err := journal.Journal(ctx, 100)
		switch {
		case err != nil:
			b.fail("journal: " + err.Error())
		case result.Skipped:
			b.journalSkips.Add(1)
		default:
			b.journalRuns.Add(1)
		}
	})
	writer := linksfreshnessstore.NewLinkWriter(SQLDB{DB: d})
	for range 4 {
		loop(time.Millisecond, func() {
			backlog, err := journal.BacklogScopes(ctx, scopes)
			if err != nil {
				b.fail("backlog: " + err.Error())
				return
			}
			for _, scopeID := range backlog {
				result, err := writer.LinkNext(ctx, scopeID)
				var retry *linksfreshnessstore.RetryError
				switch {
				case errors.As(err, &retry):
					b.linkRetries.Add(1)
				case err != nil:
					b.fail("link: " + err.Error())
				case !result.Idle:
					b.links.Add(1)
				}
			}
		})
	}
	retention := NewGenerationRetentionStore(SQLDB{DB: d})
	retention.Now = func() time.Time { return time.Now().UTC().Add(2 * time.Hour) }
	policy := GenerationRetentionPolicy{
		MinSupersededGenerations: 1, MaxSupersededAge: time.Minute,
		HardMaxSupersededAge: time.Minute, BatchGenerationLimit: 20, BatchRowLimit: 1_000_000,
	}
	loop(5*time.Millisecond, func() {
		result, err := retention.PruneSupersededGenerations(ctx, policy)
		b.retentionRuns.Add(1)
		if err != nil {
			b.fail("retention: " + err.Error())
			return
		}
		b.pruned.Add(int64(result.GenerationsPruned))
	})
	return b
}

// TestProjectorAckJournalContention runs 16 Ack workers over 24 scopes for
// 10 rounds (two generations per scope per round, 480 Acks) while a journal
// loop, four link writers and generation retention run (#7127 PR-3b). The
// mixed run acks every second generation with the journal off, as a
// rollout would. Required: no deadlock (pg_stat_database and Ack errors),
// no unexpected error anywhere, no journal row committed out of
// activation_seq order within a scope, no duplicate (scope, generation), no
// orphan, and each scope's link state on its active generation at the end.
func TestProjectorAckJournalContention(t *testing.T) {
	const scopes, rounds, workers = 24, 10, 16
	for _, mixed := range []bool{false, true} {
		t.Run(map[bool]string{false: "uniform", true: "mixed"}[mixed], func(t *testing.T) {
			d := openAckJournalDB(t, 64)
			ctx := context.Background()
			if _, err := d.ExecContext(ctx, ajAuditSQL); err != nil {
				t.Fatalf("audit trigger: %v", err)
			}
			deadlocksBefore := ajCount(t, d, `SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()`)
			for s := range scopes {
				ajSeedScope(t, d, fmt.Sprintf("c%02d", s))
			}
			stop := make(chan struct{})
			var wg sync.WaitGroup
			background := ajRunBackground(d, scopes, stop, &wg)
			on, off := ajQueue(SQLDB{DB: d}, true), ajQueue(SQLDB{DB: d}, false)
			var acked, deferred, superseded atomic.Int64
			var ackMu sync.Mutex
			var ackErrs []string
			for r := range rounds {
				var jobs []projector.ScopeGenerationWork
				for s := range scopes {
					scopeID := fmt.Sprintf("c%02d", s)
					for _, x := range []string{"a", "b"} {
						jobs = append(jobs, ajPending(t, d, scopeID, fmt.Sprintf("%s-r%d%s", scopeID, r, x), r*2+len(x)))
					}
				}
				work := make(chan projector.ScopeGenerationWork)
				var ackWG sync.WaitGroup
				for range workers {
					ackWG.Add(1)
					go func() {
						defer ackWG.Done()
						for w := range work {
							q := on
							if mixed && strings.HasSuffix(w.Generation.GenerationID, "b") {
								q = off
							}
							err := q.Ack(ctx, w, runtime.Result{})
							switch {
							case err == nil:
								acked.Add(1)
							case errors.Is(err, failure.ErrWorkAckDeferred):
								deferred.Add(1)
							case errors.Is(err, failure.ErrWorkSuperseded):
								superseded.Add(1)
							default:
								ackMu.Lock()
								ackErrs = append(ackErrs, err.Error())
								ackMu.Unlock()
							}
						}
					}()
				}
				for _, j := range jobs {
					work <- j
				}
				close(work)
				ackWG.Wait()
			}
			close(stop)
			wg.Wait()
			journal := linksfreshnessstore.NewJournalStore(SQLDB{DB: d})
			for range 3 {
				if _, err := journal.Journal(ctx, 100); err != nil {
					t.Fatalf("final journal pass: %v", err)
				}
				for s := range scopes {
					ajDrainLinks(t, d, fmt.Sprintf("c%02d", s))
				}
			}
			time.Sleep(1500 * time.Millisecond) // let backends flush pg_stat_database
			deadlocks := ajCount(t, d, `SELECT deadlocks FROM pg_stat_database WHERE datname = current_database()`) - deadlocksBefore
			duplicates := ajCount(t, d, `SELECT count(*) FROM (SELECT 1 FROM changed_since_activations GROUP BY scope_id, generation_id HAVING count(*) > 1) AS x`)
			orderViolations := ajCount(t, d, `SELECT count(*) FROM (SELECT commit_order,
                lag(commit_order) OVER (PARTITION BY scope_id ORDER BY seq) AS prior_commit FROM aj_audit) AS x WHERE commit_order < prior_commit`)
			wrongState := ajCount(t, d, `SELECT count(*) FROM ingestion_scopes AS s LEFT JOIN changed_since_scope_cursor AS c USING (scope_id)
                WHERE s.scope_id LIKE 'c%' AND COALESCE(c.state_generation_id, '') <> s.active_generation_id`)
			orphans, err := journal.Orphans(ctx)
			if err != nil {
				t.Fatalf("orphans: %v", err)
			}
			bySource := map[string]int64{}
			rows, err := d.QueryContext(ctx, `SELECT source, count(*) FROM aj_audit GROUP BY 1`)
			if err != nil {
				t.Fatalf("audit by source: %v", err)
			}
			for rows.Next() {
				var source string
				var n int64
				_ = rows.Scan(&source, &n)
				bySource[source] = n
			}
			_ = rows.Close()
			t.Logf("mixed=%v: Acks ok=%d deferred=%d superseded=%d other=%d; deadlocks=%d; duplicates=%d; commit-order violations=%d; orphans=%+v; scopes off their active generation=%d/%d; audited inserts by source=%v",
				mixed, acked.Load(), deferred.Load(), superseded.Load(), len(ackErrs), deadlocks, duplicates, orderViolations, orphans, wrongState, scopes, bySource)
			t.Logf("background: journal runs=%d skips=%d; links=%d link retries=%d; retention runs=%d pruned=%d",
				background.journalRuns.Load(), background.journalSkips.Load(), background.links.Load(), background.linkRetries.Load(),
				background.retentionRuns.Load(), background.pruned.Load())
			if len(ackErrs) > 0 || len(background.errs) > 0 {
				t.Fatalf("unexpected errors: acks %q; background %q", ackErrs, background.errs)
			}
			if deadlocks != 0 || duplicates != 0 || orderViolations != 0 || wrongState != 0 ||
				orphans.Links != 0 || orphans.Activations != 0 || orphans.HeadlessBucketGroups != 0 {
				t.Fatal("contention invariants violated (see the log line above)")
			}
			if acked.Load() == 0 || bySource[linksfreshnessstore.SourceAck] == 0 {
				t.Fatal("no Ack journaled a row: the proof is vacuous")
			}
		})
	}
}
