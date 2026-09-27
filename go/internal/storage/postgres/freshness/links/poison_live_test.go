// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore_test

import (
	"errors"
	"testing"
	"time"

	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
)

// plantLinkFailure makes every link statement of scopeID fail with a SQL
// error: a trigger refuses any state row the statement writes for it.
func (l *ledgerDB) plantLinkFailure(t *testing.T, scopeID string) {
	t.Helper()
	l.exec(t, `CREATE OR REPLACE FUNCTION planted_link_failure() RETURNS trigger LANGUAGE plpgsql AS
$$ BEGIN RAISE EXCEPTION 'planted link failure'; END $$`)
	l.exec(t, `CREATE TRIGGER planted_link_failure BEFORE INSERT ON changed_since_key_state FOR EACH ROW
WHEN (NEW.scope_id = '`+scopeID+`') EXECUTE FUNCTION planted_link_failure()`)
}

func (l *ledgerDB) removeLinkFailure(t *testing.T) {
	t.Helper()
	l.exec(t, `DROP TRIGGER planted_link_failure ON changed_since_key_state`)
}

type cursorAttempts struct {
	stateGeneration                   string
	stateSeq, attemptSeq, poisonedSeq int64
	attemptCount                      int
	nextAttemptAt, poisonedAt         *time.Time
	lastFailureClass                  *string
}

func (l *ledgerDB) attempts(t *testing.T, scopeID string) cursorAttempts {
	t.Helper()
	var c cursorAttempts
	if err := l.raw.QueryRowContext(l.ctx, `
SELECT COALESCE(state_generation_id, ''), state_activation_seq, COALESCE(attempt_activation_seq, 0),
       COALESCE(poisoned_activation_seq, 0), attempt_count, next_attempt_at, poisoned_at, last_failure_class
FROM changed_since_scope_cursor WHERE scope_id = $1`, scopeID).Scan(&c.stateGeneration, &c.stateSeq,
		&c.attemptSeq, &c.poisonedSeq, &c.attemptCount, &c.nextAttemptAt, &c.poisonedAt, &c.lastFailureClass); err != nil {
		t.Fatalf("read cursor attempts: %v", err)
	}
	return c
}

// seedPoisonScope roots scopeID at g0 and journals g1 behind it.
func (l *ledgerDB) seedPoisonScope(t *testing.T, w *linksfreshnessstore.LinkWriter, scopeID string) int64 {
	t.Helper()
	l.seedScope(t, scopeID)
	l.seedGeneration(t, scopeID, scopeID+"-g0", false, "superseded", fixtureEpoch, fixtureEpoch.Add(time.Hour))
	l.seedGeneration(t, scopeID, scopeID+"-g1", false, "active", fixtureEpoch.Add(time.Hour), time.Time{})
	l.insertFacts(t, scopeID, scopeID+"-g0", baseFacts())
	l.insertFacts(t, scopeID, scopeID+"-g1", nextFacts())
	l.journal(t, scopeID, scopeID+"-g0", "")
	l.journal(t, scopeID, scopeID+"-g1", scopeID+"-g0")
	if res := mustLink(t, w, l, scopeID); res.Kind != linksfreshnessstore.LinkKindRoot {
		t.Fatalf("root = %+v", res)
	}
	return l.queryInt(t, `SELECT activation_seq FROM changed_since_activations WHERE generation_id = $1`, scopeID+"-g1")
}

// TestFailingLinkIsPoisonedAfterMaxAttempts is the store half of gate G16a
// and gate G16c (#7127 ruling 8.10): a link that fails every time is tried
// exactly MaxAttempts times with strictly increasing backoff, then becomes
// one link_poisoned break that advances the cursor once and keeps the state;
// the next full generation links incrementally from the kept state and
// clears the marker.
func TestFailingLinkIsPoisonedAfterMaxAttempts(t *testing.T) {
	l := openLedgerDB(t)
	clock := fixtureEpoch.Add(24 * time.Hour)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	w.Now = func() time.Time { return clock }
	const scope, maxAttempts = "poison", 5
	failingSeq := l.seedPoisonScope(t, w, scope)
	stateBefore, _ := l.stateDigest(t, scope)
	l.plantLinkFailure(t, scope)

	var lastNext time.Time
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		_, err := w.LinkNext(l.ctx, scope)
		var failure *linksfreshnessstore.FailureError
		if !errors.As(err, &failure) || failure.Class != linksfreshnessstore.FailureSQLError || failure.ActivationSeq != failingSeq {
			t.Fatalf("attempt %d: LinkNext = %v, want a counting sql_error of activation %d", attempt, err, failingSeq)
		}
		record, err := w.RecordFailure(l.ctx, failure, maxAttempts)
		if err != nil || !record.Counted || record.Attempts != attempt {
			t.Fatalf("attempt %d: RecordFailure = %+v, %v", attempt, record, err)
		}
		if attempt < maxAttempts {
			// Literal ruling 8.10 backoff, not Backoff() as its own oracle.
			want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute}[attempt-1]
			if record.Poisoned || !record.NextAttemptAt.Equal(clock.Add(want)) || !record.NextAttemptAt.After(lastNext) {
				t.Fatalf("attempt %d: record %+v, want next_attempt_at %s after the failure", attempt, record, want)
			}
			stored := l.attempts(t, scope)
			if stored.nextAttemptAt == nil || !stored.nextAttemptAt.Equal(clock.Add(want)) || stored.attemptCount != attempt {
				t.Fatalf("attempt %d: cursor %+v, want attempt_count %d and next_attempt_at +%s", attempt, stored, attempt, want)
			}
			if attempt == 1 {
				if res, err := w.LinkNext(l.ctx, scope); err != nil || !res.Deferred {
					t.Fatalf("LinkNext during the backoff = %+v, %v; want deferred", res, err)
				}
			}
			lastNext = record.NextAttemptAt
			clock = record.NextAttemptAt.Add(time.Second)
			continue
		}
		if !record.Poisoned {
			t.Fatalf("attempt %d reached the limit without poisoning: %+v", attempt, record)
		}
	}
	c := l.attempts(t, scope)
	if c.stateSeq != failingSeq || c.poisonedSeq != failingSeq || c.poisonedAt == nil || c.attemptCount != 0 ||
		c.attemptSeq != 0 || c.nextAttemptAt != nil || c.stateGeneration != scope+"-g0" ||
		c.lastFailureClass == nil || *c.lastFailureClass != string(linksfreshnessstore.FailureSQLError) {
		t.Fatalf("cursor after poisoning = %+v", c)
	}
	if stateAfter, _ := l.stateDigest(t, scope); stateAfter != stateBefore {
		t.Fatalf("poisoning changed the state rows")
	}
	if n := l.queryInt(t, `SELECT count(*) FROM changed_since_links WHERE generation_id = $1`, scope+"-g1"); n != 0 {
		t.Fatalf("poisoned activation has %d link rows", n)
	}

	// G16c: recovery. The next full generation links from the kept state.
	l.removeLinkFailure(t)
	l.seedGeneration(t, scope, scope+"-g2", false, "superseded", fixtureEpoch.Add(2*time.Hour), time.Time{})
	l.insertFacts(t, scope, scope+"-g2", nextFacts())
	l.journal(t, scope, scope+"-g2", scope+"-g1")
	res := mustLink(t, w, l, scope)
	if res.Kind != linksfreshnessstore.LinkKindIncremental || res.PriorGenerationID != scope+"-g0" {
		t.Fatalf("link after poisoning = %+v, want incremental from %s-g0", res, scope)
	}
	got, n := l.stateDigest(t, scope)
	want, wantN := l.aggregateDigest(t, scope, scope+"-g2")
	if got != want || n != wantN {
		t.Fatalf("state after recovery differs from the aggregate of g2")
	}
	if c := l.attempts(t, scope); c.poisonedSeq != 0 || c.poisonedAt != nil {
		t.Fatalf("poison marker not cleared by the full link: %+v", c)
	}
}

// TestRecordFailureSkipsAHeldCursor proves the count cannot be written by a
// worker that does not hold the cursor: a held cursor means no count.
func TestRecordFailureSkipsAHeldCursor(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	seq := l.seedPoisonScope(t, w, "held-count")
	holder, err := l.raw.BeginTx(l.ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := holder.ExecContext(l.ctx, `SELECT 1 FROM changed_since_scope_cursor WHERE scope_id = 'held-count' FOR UPDATE`); err != nil {
		t.Fatalf("hold: %v", err)
	}
	record, err := w.RecordFailure(l.ctx, &linksfreshnessstore.FailureError{
		Class: linksfreshnessstore.FailureInternal, ScopeID: "held-count", ActivationSeq: seq, Err: errors.New("x"),
	}, 5)
	if err != nil || record.Counted {
		t.Fatalf("RecordFailure with the cursor held = %+v, %v; want not counted", record, err)
	}
	_ = holder.Rollback()
	if c := l.attempts(t, "held-count"); c.attemptCount != 0 {
		t.Fatalf("attempt_count = %d, want 0", c.attemptCount)
	}
}

// TestStaleFailureDoesNotCountAgainstANewerHead is review F3 (ruling 8.10
// item 4): a failure reported for an activation that another writer has since
// linked must not be counted against the scope's new head.
func TestStaleFailureDoesNotCountAgainstANewerHead(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	staleSeq := l.seedPoisonScope(t, w, "stale")
	// Another writer links the activation the stale failure is about.
	if res := mustLink(t, w, l, "stale"); res.ActivationSeq != staleSeq || res.Kind != linksfreshnessstore.LinkKindIncremental {
		t.Fatalf("link of the stale activation = %+v", res)
	}
	// A healthy newer activation is now the head.
	l.seedGeneration(t, "stale", "stale-g2", false, "superseded", fixtureEpoch.Add(2*time.Hour), time.Time{})
	l.insertFacts(t, "stale", "stale-g2", baseFacts())
	l.journal(t, "stale", "stale-g2", "stale-g1")

	record, err := w.RecordFailure(l.ctx, &linksfreshnessstore.FailureError{
		Class: linksfreshnessstore.FailureStatementTimeout, ScopeID: "stale", ActivationSeq: staleSeq, Err: errors.New("late"),
	}, 1)
	if err != nil || record.Counted || record.Poisoned {
		t.Fatalf("RecordFailure for a linked activation = %+v, %v; want not counted", record, err)
	}
	c := l.attempts(t, "stale")
	if c.attemptCount != 0 || c.attemptSeq != 0 || c.nextAttemptAt != nil || c.poisonedSeq != 0 || c.lastFailureClass != nil {
		t.Fatalf("stale failure wrote attempt state against the new head: %+v", c)
	}
	if res := mustLink(t, w, l, "stale"); res.GenerationID != "stale-g2" || res.Kind != linksfreshnessstore.LinkKindIncremental {
		t.Fatalf("new head after the stale failure = %+v, want it linked at once", res)
	}
}

// TestDigestVersionChangeReRootsTheScope is review F5 (ruling 2.4): a cursor
// built under another digest_version has no usable state, so the next full
// generation links as root, the old state rows are deleted, and the cursor
// takes the current version.
func TestDigestVersionChangeReRootsTheScope(t *testing.T) {
	l := openLedgerDB(t)
	w := linksfreshnessstore.NewLinkWriter(l.store)
	l.seedPoisonScope(t, w, "digest")
	l.exec(t, `UPDATE changed_since_scope_cursor SET digest_version = $1 WHERE scope_id = 'digest'`,
		linksfreshnessstore.DigestVersion-1)
	l.exec(t, `INSERT INTO changed_since_key_state (scope_id, fact_category, stable_fact_key, fact_kind, state)
VALUES ('digest', 'facts', 'old-digest-only-key', 'repository', sha256('old'::bytea))`)

	res := mustLink(t, w, l, "digest")
	if res.Kind != linksfreshnessstore.LinkKindRoot || res.PriorGenerationID != "" || res.GenerationID != "digest-g1" {
		t.Fatalf("link after a digest_version change = %+v, want a root of digest-g1", res)
	}
	if n := l.queryInt(t, `SELECT count(*) FROM changed_since_key_state WHERE stable_fact_key = 'old-digest-only-key'`); n != 0 {
		t.Fatalf("the re-root kept %d state rows of the old digest", n)
	}
	got, n := l.stateDigest(t, "digest")
	want, wantN := l.aggregateDigest(t, "digest", "digest-g1")
	if got != want || n != wantN {
		t.Fatalf("state after the re-root differs from the aggregate of digest-g1")
	}
	if v := l.queryInt(t, `SELECT digest_version FROM changed_since_scope_cursor WHERE scope_id = 'digest'`); v != linksfreshnessstore.DigestVersion {
		t.Fatalf("cursor digest_version = %d, want %d", v, linksfreshnessstore.DigestVersion)
	}
}
