// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package links_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eshu-hq/eshu/go/internal/reducer/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// retentionChildEnv selects a child role of
// TestLinkAndRetentionProcessesRace: "link" or "retention"; the database is
// named by childDSNEnv.
const retentionChildEnv = "ESHU_CHANGED_SINCE_RETENTION_RACE_ROLE"

// retentionRaceRowLimit fits one generation's 2,000 facts and a normal link,
// but not a generation plus a link that rewrote every key.
const retentionRaceRowLimit = 2100

// changeEvery makes every third scope rewrite all 2,000 keys between
// generations, so its links (about 2,000 deltas) are over
// retentionRaceRowLimit together with either generation's facts; the other
// scopes change one key in 17.
func changeEvery(scope int) int {
	if scope%3 == 0 {
		return 1
	}
	return 17
}

type retentionRaceTotals struct {
	Linked, Breaks, PrunedBeforeLink, GenerationLocked, LockTimeouts, Rebases, Batches, GenerationsPruned, OverLimitBatches int
	Deadlocks, Updates                                                                                                      int
	MaxBatchMillis, MaxRetryMillis                                                                                          int64
}

// outcomeCounter wraps the production writer and counts the outcomes
// retention can cause.
type outcomeCounter struct {
	*store.LinkWriter
	mu                                                            sync.Mutex
	prunedBeforeLink, genLocked, lockTimeouts, rebases, deadlocks int
	maxRetry                                                      time.Duration
}

func (c *outcomeCounter) LinkNext(ctx context.Context, scopeID string) (store.LinkResult, error) {
	start := time.Now()
	result, err := c.LinkWriter.LinkNext(ctx, scopeID)
	elapsed := time.Since(start)
	c.mu.Lock()
	defer c.mu.Unlock()
	reason, retry := store.RetryReasonOf(err)
	if retry {
		c.maxRetry = max(c.maxRetry, elapsed)
	}
	switch {
	case retry && reason == store.RetryGenerationLocked:
		c.genLocked++
	case retry && reason == store.RetryGenerationLockTimeout:
		c.lockTimeouts++
	case isDeadlock(err):
		c.deadlocks++
	}
	if result.Break == store.BreakPrunedBeforeLink {
		c.prunedBeforeLink++
	}
	if result.Break == store.BreakPriorPruned {
		c.rebases++
	}
	return result, err
}

func runRetentionRaceChild(t *testing.T, role, dsn string) {
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("child open: %v", err)
	}
	defer func() { _ = raw.Close() }()
	db := postgres.SQLDB{DB: raw}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var totals retentionRaceTotals
	switch role {
	case "link":
		counter := &outcomeCounter{LinkWriter: store.NewLinkWriter(db)}
		runner := &links.Runner{
			Linker:  counter,
			Journal: store.NewJournalStore(db),
			Config:  links.Config{Workers: 4, BackfillScopesPerCycle: -1},
		}
		for idle := 0; idle < 5 && ctx.Err() == nil; {
			result, err := runner.RunOnce(ctx)
			if err != nil {
				t.Fatalf("link cycle: %v", err)
			}
			totals.Linked += result.Linked
			totals.Breaks += result.Breaks
			if result.Linked+result.Breaks+result.Retries+result.Failures == 0 {
				idle++
				time.Sleep(50 * time.Millisecond)
			} else {
				idle = 0
			}
		}
		totals.PrunedBeforeLink, totals.GenerationLocked, totals.Rebases = counter.prunedBeforeLink, counter.genLocked, counter.rebases
		totals.LockTimeouts, totals.Deadlocks, totals.MaxRetryMillis = counter.lockTimeouts, counter.deadlocks, counter.maxRetry.Milliseconds()
	case "retention":
		retention := postgres.NewGenerationRetentionStore(db)
		// 2,000 facts per generation fit the limit alone; a link over the
		// limit makes its generation a batch of one (arb-7127-3d-b).
		policy := postgres.GenerationRetentionPolicy{
			MinSupersededGenerations: 0, MaxSupersededAge: time.Hour, BatchGenerationLimit: 4,
			BatchRowLimit: retentionRaceRowLimit, PolicyScope: "global", PolicyRevision: "7127-pr3d-p7",
		}
		for idle := 0; idle < 5 && ctx.Err() == nil; {
			result, err := retention.PruneSupersededGenerations(ctx, policy)
			if isDeadlock(err) {
				totals.Deadlocks++
				continue
			}
			if err != nil {
				t.Fatalf("retention batch: %v", err)
			}
			totals.Batches++
			totals.GenerationsPruned += result.GenerationsPruned
			totals.MaxBatchMillis = max(totals.MaxBatchMillis, result.Duration.Milliseconds())
			if result.RowsOverLimit > 0 {
				totals.OverLimitBatches++
			}
			if result.GenerationsPruned == 0 {
				idle++
				time.Sleep(20 * time.Millisecond)
			} else {
				idle = 0
			}
		}
	case "updater":
		// The third actor of W10 (arbiter ruling arb-7127-3e-wait): non-key
		// updates of every race generation, the prior X and the activating G
		// alike, committed one at a time for as long as there is work, so a
		// generation lock can find an update chain.
		deadline := time.Now().Add(20 * time.Second)
		for i := 0; time.Now().Before(deadline) && ctx.Err() == nil; i++ {
			gen := fmt.Sprintf("prace-%02d-g%d", i%24, (i/24)%3)
			_, err := raw.ExecContext(ctx, `UPDATE scope_generations SET ingested_at = ingested_at + interval '1 microsecond'
WHERE generation_id = $1`, gen)
			switch {
			case isDeadlock(err):
				totals.Deadlocks++
			case err != nil:
				t.Fatalf("updater: %v", err)
			default:
				totals.Updates++
			}
			if i%24 == 23 {
				var pending int
				if err := raw.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM changed_since_activations AS a
    LEFT JOIN changed_since_scope_cursor AS c ON c.scope_id = a.scope_id
    WHERE a.scope_id LIKE 'prace-%' AND a.activation_seq > COALESCE(c.state_activation_seq, 0))
  + (SELECT count(*) FROM scope_generations WHERE scope_id LIKE 'prace-%' AND status = 'superseded')`).Scan(&pending); err != nil {
					t.Fatalf("updater pending: %v", err)
				}
				if pending == 0 {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
		}
	default:
		t.Fatalf("unknown child role %q", role)
	}
	b, _ := json.Marshal(totals)
	fmt.Println("CHILD " + string(b))
}

// TestLinkAndRetentionProcessesRace is P7 of arbiter ruling arb-7127-3d and
// PB5 of arb-7127-3d-b, on the compiled test binary: one OS process runs the
// production link runner and two others run generation retention batches (at
// most four generations, BatchRowLimit 2,100) over 24 scopes whose g0 and g1
// are prunable while their activations are being linked. Every third scope
// writes links over the limit, so its generations are pruned in batches of
// one. Every prunable generation is pruned (none starves), at least one batch
// is over the limit, and:
//   - (a) no link names a pruned generation on its generation side: the
//     writer's FOR KEY SHARE kept retention off every generation it linked;
//   - (b) every retention batch returned within 1 s while links ran;
//   - (c) the writer's generation_locked retries and pruned_before_link
//     breaks are reported (the ways retention shows through to the writer);
//   - (d) no link-delta or bucket-count row is without its link row, and no
//     link names a pruned prior: the writer fences the prior too (PR-3e,
//     C1), so a race on a prior ends in a skip, a generation_locked or a
//     rebase, and the orphan probe is zero;
//   - no generation is pruned twice: one retention event per generation.
func TestLinkAndRetentionProcessesRace(t *testing.T) {
	if role := os.Getenv(retentionChildEnv); role != "" {
		runRetentionRaceChild(t, role, os.Getenv(childDSNEnv))
		return
	}
	l := openLedgerDB(t)
	const scopes = 24
	old := fixtureEpoch.Add(-72 * time.Hour)
	for i := range scopes {
		scopeID := fmt.Sprintf("prace-%02d", i)
		l.seedScope(t, scopeID)
		for g := range 3 {
			gen := fmt.Sprintf("%s-g%d", scopeID, g)
			if g < 2 {
				l.seedGeneration(t, scopeID, gen, false, "superseded", old.Add(time.Duration(g)*time.Hour), old.Add(time.Duration(g+1)*time.Hour))
			} else {
				l.seedGeneration(t, scopeID, gen, false, "active", old.Add(2*time.Hour), time.Time{})
			}
			l.exec(t, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, source_uri, observed_at, ingested_at, is_tombstone, payload)
SELECT $2 || '/' || i, $1, $2, 'content_entity', 'ent:' || i, 'git', 'ent:' || i, 'f' || (i / 50) || '.go',
       $3, $3, FALSE, jsonb_build_object('n', i, 'v', CASE WHEN i % $5 = 0 THEN $4 ELSE 0 END, 'body', repeat('x', 200))
FROM generate_series(1, 2000) AS i`, scopeID, gen, fixtureEpoch, g, changeEvery(i))
		}
		l.exec(t, `UPDATE ingestion_scopes SET active_generation_id = $2 WHERE scope_id = $1`, scopeID, scopeID+"-g2")
		l.journal(t, scopeID, scopeID+"-g0", "")
		l.journal(t, scopeID, scopeID+"-g1", scopeID+"-g0")
		l.journal(t, scopeID, scopeID+"-g2", scopeID+"-g1")
	}

	// Two retention processes race each other and the link runner (PC3 of
	// arbiter ruling arb-7127-3d-c).
	roles := []string{"link", "retention", "retention", "updater"}
	outputs := make([]bytes.Buffer, len(roles))
	errs := make([]error, len(roles))
	var wg sync.WaitGroup
	for i, role := range roles {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run", "^TestLinkAndRetentionProcessesRace$", "-test.v")
			cmd.Env = append(os.Environ(), childDSNEnv+"="+l.dsn, retentionChildEnv+"="+role)
			cmd.Stdout = &outputs[i]
			cmd.Stderr = &outputs[i]
			errs[i] = cmd.Run()
		}()
	}
	wg.Wait()
	totals := make([]retentionRaceTotals, len(roles))
	for i, role := range roles {
		if errs[i] != nil {
			t.Fatalf("%s child: %v\n%s", role, errs[i], outputs[i].String())
		}
		found := false
		scanner := bufio.NewScanner(&outputs[i])
		for scanner.Scan() {
			if line := scanner.Text(); strings.HasPrefix(line, "CHILD ") {
				found = json.Unmarshal([]byte(strings.TrimPrefix(line, "CHILD ")), &totals[i]) == nil
			}
		}
		if !found {
			t.Fatalf("%s child printed no totals:\n%s", role, outputs[i].String())
		}
		t.Logf("%s child: %+v", role, totals[i])
	}
	link, retention := totals[0], totals[1]
	retention.Batches += totals[2].Batches
	retention.GenerationsPruned += totals[2].GenerationsPruned
	retention.OverLimitBatches += totals[2].OverLimitBatches
	retention.MaxBatchMillis = max(retention.MaxBatchMillis, totals[2].MaxBatchMillis)
	updater := totals[3]
	// W10 (arbiter ruling arb-7127-3e-wait): no session saw a deadlock, and
	// every link that ended in a retry answered in under a second.
	if n := link.Deadlocks + retention.Deadlocks + totals[2].Deadlocks + updater.Deadlocks; n != 0 {
		t.Fatalf("W10: %d deadlock errors (40P01) across the sessions, want 0", n)
	}
	if link.MaxRetryMillis >= 1000 {
		t.Fatalf("W10: a link that ended in a retry took %d ms, want under 1 s", link.MaxRetryMillis)
	}
	if retention.GenerationsPruned != 2*scopes {
		t.Fatalf("retention pruned %d generations, want %d (g0 and g1 of every scope; none starved)", retention.GenerationsPruned, 2*scopes)
	}
	if n := l.queryInt(t, `SELECT count(*) FROM (SELECT generation_id_hash FROM generation_retention_events
GROUP BY generation_id_hash HAVING count(*) > 1) AS twice`); n != 0 {
		t.Fatalf("%d generations were pruned by both retention processes (more than one event)", n)
	}
	if n := l.queryInt(t, `SELECT count(*) FROM generation_retention_events`); n != int64(2*scopes) {
		t.Fatalf("%d retention events, want one per pruned generation (%d)", n, 2*scopes)
	}
	if retention.OverLimitBatches == 0 {
		t.Fatal("no retention batch was over the limit; the over-limit links did not reach a prune")
	}
	// (a)
	if n := l.queryInt(t, `SELECT count(*) FROM changed_since_links AS l
WHERE NOT EXISTS (SELECT 1 FROM scope_generations AS g WHERE g.generation_id = l.generation_id)`); n != 0 {
		t.Fatalf("(a) %d links name a pruned generation on their generation side", n)
	}
	// (b)
	if retention.MaxBatchMillis >= 1000 {
		t.Fatalf("(b) a retention batch took %d ms while links ran, want under 1 s", retention.MaxBatchMillis)
	}
	// (d)
	if n := l.queryInt(t, `SELECT count(*) FROM changed_since_link_deltas AS d
WHERE NOT EXISTS (SELECT 1 FROM changed_since_links AS l WHERE l.scope_id = d.scope_id
                    AND l.generation_id = d.generation_id AND l.prior_generation_id = d.prior_generation_id)`); n != 0 {
		t.Fatalf("(d) %d delta rows have no link row", n)
	}
	if n := l.queryInt(t, `SELECT count(*) FROM (SELECT DISTINCT scope_id, generation_id, prior_generation_id
                         FROM changed_since_link_bucket_counts) AS b
WHERE NOT EXISTS (SELECT 1 FROM changed_since_links AS l WHERE l.scope_id = b.scope_id
                    AND l.generation_id = b.generation_id AND l.prior_generation_id = b.prior_generation_id)`); n != 0 {
		t.Fatalf("(d) %d bucket groups have no link row", n)
	}
	orphans, err := store.NewJournalStore(l.store).Orphans(l.ctx)
	if err != nil {
		t.Fatalf("orphan probe: %v", err)
	}
	if orphans != (store.LedgerOrphans{}) {
		t.Fatalf("(d) orphan probe = %+v after the race, want zeros (no ledger row names a pruned generation)", orphans)
	}
	// (c)
	t.Logf("P7/W10: %d scopes, %d retention batches (max %d ms), %d links (%d rebases), %d breaks (%d pruned_before_link), %d generation_locked, %d generation_lock_timeout (slowest retry %d ms), %d non-key updates, 0 deadlocks, orphan probe %+v, %d over-limit batches",
		scopes, retention.Batches, retention.MaxBatchMillis, link.Linked, link.Rebases, link.Breaks, link.PrunedBeforeLink, link.GenerationLocked, link.LockTimeouts, link.MaxRetryMillis, updater.Updates, orphans, retention.OverLimitBatches)
}

// isDeadlock reports SQLSTATE 40P01 in err's chain.
func isDeadlock(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "40P01"
}
