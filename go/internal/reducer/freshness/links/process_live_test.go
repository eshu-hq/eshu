// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package links_test

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	store "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// childDSNEnv switches TestTwoProcessRunnersLinkEachActivationOnce into its
// child role: one runner process draining the database it names.
const childDSNEnv = "ESHU_CHANGED_SINCE_LINK_CHILD_DSN"

type childTotals struct {
	Linked, Breaks, Poisoned, Retries, Failures, Cycles, CursorLocked, SlowLosers int
}

// raceCounter wraps the production writer and counts the cursor_locked
// outcomes the runner sees, and how many took a second or more.
type raceCounter struct {
	*store.LinkWriter
	mu                   sync.Mutex
	cursorLocked, slower int
}

func (c *raceCounter) LinkNext(ctx context.Context, scopeID string) (store.LinkResult, error) {
	began := time.Now()
	result, err := c.LinkWriter.LinkNext(ctx, scopeID)
	if reason, ok := store.RetryReasonOf(err); ok && reason == store.RetryCursorLocked {
		c.mu.Lock()
		c.cursorLocked++
		if time.Since(began) >= time.Second {
			c.slower++
		}
		c.mu.Unlock()
	}
	return result, err
}

// runChild drains the backlog with one runner until three idle cycles in a
// row, then prints its totals.
func runChild(t *testing.T, dsn string) {
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("child open: %v", err)
	}
	defer func() { _ = raw.Close() }()
	db := postgres.SQLDB{DB: raw}
	counter := &raceCounter{LinkWriter: store.NewLinkWriter(db)}
	runner := &links.Runner{
		Linker:  counter,
		Journal: store.NewJournalStore(db),
		Config:  links.Config{Workers: 4, BackfillScopesPerCycle: -1},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var totals childTotals
	idle := 0
	for idle < 3 && ctx.Err() == nil {
		result, err := runner.RunOnce(ctx)
		if err != nil {
			t.Fatalf("child cycle: %v", err)
		}
		totals.Cycles++
		totals.Linked += result.Linked
		totals.Breaks += result.Breaks
		totals.Poisoned += result.Poisoned
		totals.Retries += result.Retries
		totals.Failures += result.Failures
		if result.Linked+result.Breaks+result.Retries+result.Failures == 0 {
			idle++
			time.Sleep(50 * time.Millisecond)
		} else {
			idle = 0
		}
	}
	totals.CursorLocked, totals.SlowLosers = counter.cursorLocked, counter.slower
	b, _ := json.Marshal(totals)
	fmt.Println("CHILD " + string(b))
}

// TestTwoProcessRunnersLinkEachActivationOnce is gates G9 and G16d on the
// runner (#7127 ruling 8.10): two OS processes of the compiled test binary,
// each running the production Runner, drain one database. Every activation
// ends as exactly one link or one break, no attempt is counted, and the
// losers of the races report cursor_locked (non-counting) and move on.
func TestTwoProcessRunnersLinkEachActivationOnce(t *testing.T) {
	if dsn := os.Getenv(childDSNEnv); dsn != "" {
		runChild(t, dsn)
		return
	}
	l := openLedgerDB(t)
	const scopes = 40
	for i := range scopes {
		scopeID := fmt.Sprintf("race-%02d", i)
		l.seedScope(t, scopeID)
		l.seedGeneration(t, scopeID, scopeID+"-g0", false, "superseded", fixtureEpoch, fixtureEpoch.Add(time.Hour))
		l.seedGeneration(t, scopeID, scopeID+"-g1", false, "superseded", fixtureEpoch.Add(time.Hour), fixtureEpoch.Add(2*time.Hour))
		l.seedGeneration(t, scopeID, scopeID+"-g2", true, "active", fixtureEpoch.Add(2*time.Hour), time.Time{})
		for g, variant := range []int{0, 1} {
			l.exec(t, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
    source_fact_key, source_uri, observed_at, ingested_at, is_tombstone, payload)
SELECT $2 || '/' || i, $1, $2, 'content_entity', 'ent:' || i, 'git', 'ent:' || i, 'f' || (i / 50) || '.go',
       $3, $3, FALSE, jsonb_build_object('n', i, 'v', CASE WHEN i % 31 = 0 THEN $4 ELSE 0 END, 'body', repeat('x', 200))
FROM generate_series(1, 4000) AS i`, scopeID, fmt.Sprintf("%s-g%d", scopeID, g), fixtureEpoch, variant)
		}
		l.journal(t, scopeID, scopeID+"-g0", "")
		l.journal(t, scopeID, scopeID+"-g1", scopeID+"-g0")
		l.journal(t, scopeID, scopeID+"-g2", scopeID+"-g1")
	}

	var wg sync.WaitGroup
	outputs := make([]bytes.Buffer, 2)
	errs := make([]error, 2)
	for p := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run", "^TestTwoProcessRunnersLinkEachActivationOnce$", "-test.v")
			cmd.Env = append(os.Environ(), childDSNEnv+"="+l.dsn)
			cmd.Stdout = &outputs[p]
			cmd.Stderr = &outputs[p]
			errs[p] = cmd.Run()
		}()
	}
	wg.Wait()
	var sum childTotals
	for p := range 2 {
		if errs[p] != nil {
			t.Fatalf("child %d: %v\n%s", p, errs[p], outputs[p].String())
		}
		var got childTotals
		found := false
		scanner := bufio.NewScanner(&outputs[p])
		for scanner.Scan() {
			if line := scanner.Text(); strings.HasPrefix(line, "CHILD ") {
				found = json.Unmarshal([]byte(strings.TrimPrefix(line, "CHILD ")), &got) == nil
			}
		}
		if !found {
			t.Fatalf("child %d printed no totals", p)
		}
		t.Logf("child %d: %+v", p, got)
		sum.Linked += got.Linked
		sum.Breaks += got.Breaks
		sum.Poisoned += got.Poisoned
		sum.Retries += got.Retries
		sum.Failures += got.Failures
		sum.CursorLocked += got.CursorLocked
		sum.SlowLosers += got.SlowLosers
	}
	activations := l.queryInt(t, `SELECT count(*) FROM changed_since_activations`)
	if int64(sum.Linked+sum.Breaks) != activations {
		t.Fatalf("G16d: %d links + %d breaks over two processes, want exactly one outcome for each of %d activations",
			sum.Linked, sum.Breaks, activations)
	}
	if links := l.queryInt(t, `SELECT count(*) FROM changed_since_links`); links != int64(sum.Linked) {
		t.Fatalf("G16d: %d link rows for %d reported links", links, sum.Linked)
	}
	if sum.Failures != 0 || sum.Poisoned != 0 {
		t.Fatalf("G9: races produced failures (%d) or poisonings (%d)", sum.Failures, sum.Poisoned)
	}
	if counted := l.queryInt(t, `SELECT count(*) FROM changed_since_scope_cursor WHERE attempt_count > 0`); counted != 0 {
		t.Fatalf("G9: %d cursors counted an attempt; lock misses are non-counting", counted)
	}
	behind := l.queryInt(t, `
SELECT count(*) FROM changed_since_scope_cursor AS c
WHERE c.state_activation_seq <> (SELECT max(activation_seq) FROM changed_since_activations AS a WHERE a.scope_id = c.scope_id)`)
	if behind != 0 {
		t.Fatalf("G9: %d cursors are not at their last activation", behind)
	}
	if sum.CursorLocked < 20 {
		t.Fatalf("G9: only %d cursor_locked races were observed across the two processes, want at least 20", sum.CursorLocked)
	}
	if sum.SlowLosers != 0 {
		t.Fatalf("G9: %d cursor_locked losers took a second or more", sum.SlowLosers)
	}
	t.Logf("G9/G16d: %d activations, %d links, %d breaks; %d cursor_locked races, all under 1s; %d non-counting misses in total",
		activations, sum.Linked, sum.Breaks, sum.CursorLocked, sum.Retries)
}
