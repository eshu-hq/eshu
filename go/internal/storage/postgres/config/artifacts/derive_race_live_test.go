// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package artifacts_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/config/artifacts"
)

// composeBody is a Docker Compose file whose one env_file row names k, so a
// stored row says which content write the derive read.
func composeBody(k int) string {
	return fmt.Sprintf("services:\n  app:\n    image: app\n    env_file: env/v%d.env\n", k)
}

var envFileGeneration = regexp.MustCompile(`^env/v(\d+)\.env$`)

// rowGeneration returns the k of the env_file row of rows, or -1.
func rowGeneration(t *testing.T, rows []artifacts.Row) int {
	t.Helper()
	for _, row := range rows {
		if match := envFileGeneration.FindStringSubmatch(row.RawPath); match != nil {
			k, _ := strconv.Atoi(match[1])
			return k
		}
	}
	return -1
}

// waitForAdvisoryWaiter polls pg_stat_activity until a session of this
// database waits on an advisory lock.
func waitForAdvisoryWaiter(ctx context.Context, t *testing.T, db *sql.DB) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int
		if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM pg_stat_activity
WHERE datname = current_database() AND wait_event_type = 'Lock' AND wait_event = 'advisory'`).Scan(&waiting); err != nil {
			t.Fatalf("read advisory waiters: %v", err)
		}
		if waiting > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no session waited on the repository advisory lock within 10s")
}

// heldDerive starts a derive of repoID that stops right after it is granted
// the repository lock, and returns a release function and the result channel.
func heldDerive(ctx context.Context, t *testing.T, db *sql.DB, repoID string) (release func(), done <-chan artifacts.Result) {
	t.Helper()
	return heldDeriveAt(ctx, t, db, repoID, testExtractorVersion)
}

// heldDeriveAt is heldDerive for a deriver of the given extractor version.
func heldDeriveAt(ctx context.Context, t *testing.T, db *sql.DB, repoID string, extractorVersion int) (release func(), done <-chan artifacts.Result) {
	t.Helper()
	locked, proceed := make(chan struct{}), make(chan struct{})
	results := make(chan artifacts.Result, 1)
	deriver := newLiveDeriver(t, db, extractorVersion)
	artifacts.SetDeriveHooks(deriver, artifacts.DeriveHooks{AfterLock: func(context.Context) {
		close(locked)
		<-proceed
	}})
	go func() {
		results <- deriver.DeriveRepository(ctx, artifacts.Request{RepoID: repoID, Trigger: artifacts.TriggerWrite})
	}()
	select {
	case <-locked:
	case <-time.After(10 * time.Second):
		t.Fatal("held derive never took the repository lock")
	}
	return func() { close(proceed) }, results
}

// TestDeriveTwoDerivesOfOneRepositorySerializeLive: a second derive of one
// repository waits on the advisory lock while the first holds it, then reads
// the first one's state and is superseded. A repeated unsynchronized race
// ends every round with exactly one ok and one superseded.
func TestDeriveTwoDerivesOfOneRepositorySerializeLive(t *testing.T) {
	ctx, db := openDatabase(t)
	putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(0))

	release, first := heldDerive(ctx, t, db, "repo-a")
	second := make(chan artifacts.Result, 1)
	secondDeriver := newLiveDeriver(t, db, testExtractorVersion)
	go func() {
		second <- secondDeriver.DeriveRepository(ctx, artifacts.Request{RepoID: "repo-a", Trigger: artifacts.TriggerRepair})
	}()
	waitForAdvisoryWaiter(ctx, t, db)
	release()
	if got := (<-first).Outcome; got != artifacts.OutcomeOK {
		t.Fatalf("first derive = %q, want ok", got)
	}
	if got := (<-second).Outcome; got != artifacts.OutcomeSuperseded {
		t.Fatalf("second derive = %q, want superseded", got)
	}

	for round := 1; round <= 20; round++ {
		putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(round))
		var wg sync.WaitGroup
		outcomes := make([]artifacts.Outcome, 2)
		derivers := []*artifacts.Deriver{newLiveDeriver(t, db, testExtractorVersion), newLiveDeriver(t, db, testExtractorVersion)}
		for i := range outcomes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				outcomes[i] = derivers[i].DeriveRepository(ctx, artifacts.Request{RepoID: "repo-a", Trigger: artifacts.TriggerRepair}).Outcome
			}()
		}
		wg.Wait()
		ok, superseded := 0, 0
		for _, outcome := range outcomes {
			switch outcome {
			case artifacts.OutcomeOK:
				ok++
			case artifacts.OutcomeSuperseded:
				superseded++
			}
		}
		if ok != 1 || superseded != 1 {
			t.Fatalf("round %d outcomes = %v, want one ok and one superseded", round, outcomes)
		}
		if got := rowGeneration(t, storedRows(ctx, t, db, "repo-a")); got != round {
			t.Fatalf("round %d stored generation = %d", round, got)
		}
		state := stateOf(ctx, t, db, "repo-a")
		if state == nil || state.ContentVersion == nil || *state.ContentVersion != versionOf(ctx, t, db, "repo-a") {
			t.Fatalf("round %d state = %+v, want the current version", round, state)
		}
	}
}

// TestDeriveRacingContentWritesNeverLabelsNewerContentLive: derives race a
// stream of content writes. Every stored (state, rows) pair, read in one
// snapshot, holds rows from content at least as new as the version it is
// labelled with: a derive never labels rows with a version newer than the
// content it read. After the writes stop, one derive converges.
func TestDeriveRacingContentWritesNeverLabelsNewerContentLive(t *testing.T) {
	ctx, db := openDatabase(t)
	putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(0))
	baseVersion := versionOf(ctx, t, db, "repo-a")
	const writes = 40
	// Seed one successful derive so a labelled state row exists before the
	// first snapshot check; without it every check could land before any
	// derive wins the race and the test would assert nothing.
	if seed := newLiveDeriver(t, db, testExtractorVersion).DeriveRepository(ctx, artifacts.Request{RepoID: "repo-a", Trigger: artifacts.TriggerRepair}); seed.Outcome != artifacts.OutcomeOK {
		t.Fatalf("seed derive = %+v, want ok", seed)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var outcomeMu sync.Mutex
	outcomes := map[artifacts.Outcome]int{}
	for range 3 {
		wg.Add(1)
		deriver := newLiveDeriver(t, db, testExtractorVersion)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				outcome := deriver.DeriveRepository(ctx, artifacts.Request{RepoID: "repo-a", Trigger: artifacts.TriggerRepair}).Outcome
				outcomeMu.Lock()
				outcomes[outcome]++
				outcomeMu.Unlock()
			}
		}()
	}
	checks := 0
	check := func() {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
		if err != nil {
			t.Fatalf("begin snapshot: %v", err)
		}
		defer func() { _ = tx.Rollback() }()
		var label sql.NullInt64
		err = tx.QueryRowContext(ctx, `SELECT content_version FROM config_artifact_repo_state WHERE repo_id = 'repo-a'`).Scan(&label)
		if errors.Is(err, sql.ErrNoRows) {
			return // no state row yet
		}
		if err != nil {
			t.Fatalf("read state: %v", err)
		}
		if !label.Valid {
			return // no successful derive yet
		}
		labelled := int(label.Int64 - baseVersion)
		if got := rowGeneration(t, storedRows(ctx, t, tx, "repo-a")); got < labelled {
			t.Fatalf("rows from content write %d are labelled version %d (content write %d)", got, label.Int64, labelled)
		}
		checks++
	}
	for k := 1; k <= writes; k++ {
		putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(k))
		check()
		time.Sleep(2 * time.Millisecond)
		check()
	}
	close(stop)
	wg.Wait()
	if checks == 0 {
		t.Fatal("no snapshot check ran: the stored (state, rows) invariant was never inspected")
	}
	if got := versionOf(ctx, t, db, "repo-a"); got != baseVersion+writes {
		t.Fatalf("version = %d, want %d: one bump per content write", got, baseVersion+writes)
	}
	result := newLiveDeriver(t, db, testExtractorVersion).DeriveRepository(ctx, artifacts.Request{RepoID: "repo-a", Trigger: artifacts.TriggerRepair})
	state := stateOf(ctx, t, db, "repo-a")
	if state == nil || state.ContentVersion == nil || *state.ContentVersion != baseVersion+writes {
		t.Fatalf("final derive %+v left state %+v, want content version %d", result, state, baseVersion+writes)
	}
	if got := rowGeneration(t, storedRows(ctx, t, db, "repo-a")); got != writes {
		t.Fatalf("final stored generation = %d, want %d", got, writes)
	}
	if outcomes[artifacts.OutcomeError]+outcomes[artifacts.OutcomePanic]+outcomes[artifacts.OutcomeTimeout] > 0 {
		t.Fatalf("racing derives failed: %v", outcomes)
	}
	t.Logf("racing derives: %v over %d content writes, %d snapshot checks", outcomes, writes, checks)
}

// TestDeriveTwoRepositoriesRunInParallelLive: the lock is per repository, so
// a derive of repo-b completes while a derive of repo-a holds its lock.
func TestDeriveTwoRepositoriesRunInParallelLive(t *testing.T) {
	ctx, db := openDatabase(t)
	putFile(ctx, t, db, "repo-a", "docker-compose.yml", composeBody(1))
	putFile(ctx, t, db, "repo-b", "docker-compose.yml", composeBody(2))

	release, held := heldDerive(ctx, t, db, "repo-a")
	resultB := make(chan artifacts.Result, 1)
	deriverB := newLiveDeriver(t, db, testExtractorVersion)
	go func() {
		resultB <- deriverB.DeriveRepository(ctx, artifacts.Request{RepoID: "repo-b", Trigger: artifacts.TriggerWrite})
	}()
	select {
	case result := <-resultB:
		if result.Outcome != artifacts.OutcomeOK {
			t.Fatalf("repo-b derive = %+v, want ok", result)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("repo-b derive blocked behind repo-a's lock")
	}
	release()
	if result := <-held; result.Outcome != artifacts.OutcomeOK {
		t.Fatalf("repo-a derive = %+v, want ok", result)
	}
	if rowGeneration(t, storedRows(ctx, t, db, "repo-a")) != 1 || rowGeneration(t, storedRows(ctx, t, db, "repo-b")) != 2 {
		t.Fatal("each repository must hold its own rows")
	}
}
