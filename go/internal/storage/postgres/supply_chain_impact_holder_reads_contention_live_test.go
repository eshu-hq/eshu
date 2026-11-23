// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

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

	"github.com/eshu-hq/eshu/go/internal/facts"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

const (
	contentionChurnScope      = "scope:11197:churn"
	contentionChurnGeneration = "generation:11197:churn"
)

// contentionFatal reports whether err is a lock failure no reader may ever see.
func contentionFatal(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "40P01") || strings.Contains(msg, "deadlock detected")
}

// TestSupplyChainImpactSupportsHolderReadsUnderWriterContentionLive runs the
// moved holder reads against the real writer and a cascade-deleting churn scope at
// once (#11197), on GINs built with fastupdate = off (migrations 341 and 342), so
// the writer maintains them in place and no pending list exists to flush. Two scopes always hold the CVE
// while their finding sets are replaced over and over (each replacement
// tombstones the older facts and inserts new ones in one transaction), so a read
// that ever misses one of them saw a torn replacement: the invariant is atomic
// visibility, not a particular timing. No read may fail, none may report a
// deadlock, and none may report the supports not ready (the writer keeps them
// ready). After the load quiesces every read equals the retired fact oracle and
// the supports mirror the live facts. Run it with -race -count=20 and
// GOMAXPROCS=2.
func TestSupplyChainImpactSupportsHolderReadsUnderWriterContentionLive(t *testing.T) {
	ctx, db := openReplaceSetLiveDB(t)
	replaceSetSeedSecondScope(t, ctx, db)
	settleSupplyChainImpactSupports(t, ctx, db)
	writer := replaceSetWriter(db)
	store := postgres.SupplyChainImpactFanOutStore{
		Beginner: postgres.SQLDB{DB: db}, DB: postgres.SQLDB{DB: db}, LeaseOwner: fanOutLiveOwner,
	}
	ranges := supplychaincore.HolderKeyRanges()
	cve := []string{replaceSetLiveCVE}

	// Both scopes hold the CVE before any reader starts.
	for _, scoped := range []bool{false, true} {
		write := replaceSetWrite(fmt.Sprintf("intent:11197:contention-base-%v", scoped), replaceSetFinding("repository:r_11197_base"))
		if scoped {
			write.ScopeID, write.GenerationID = replaceSetSecondScope, replaceSetSecondGeneration
		}
		if _, err := writer.WriteSupplyChainImpactFindings(ctx, write); err != nil {
			t.Fatalf("baseline write: %v", err)
		}
	}

	var stop atomic.Bool
	var readers, writers sync.WaitGroup
	failures := make(chan error, 64)
	fail := func(format string, args ...any) {
		select {
		case failures <- fmt.Errorf(format, args...):
		default:
		}
	}

	var reads atomic.Int64
	check := func(kind string, scopes []string, err error) {
		reads.Add(1)
		if err != nil {
			fail("%s read failed (deadlock=%v, not ready=%v): %v", kind, contentionFatal(err), isSupportsNotReady(err), err)
			return
		}
		seen := map[string]bool{}
		for _, s := range scopes {
			seen[s] = true
		}
		for _, want := range []string{replaceSetLiveScope, replaceSetSecondScope} {
			if !seen[want] {
				fail("%s read = %v, missing %s: a torn replacement was visible", kind, scopes, want)
			}
		}
	}
	for i := 0; i < 3; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for !stop.Load() {
				scopes, err := store.FindHolderScopes(ctx, "scope:none", cve)
				check("holders", scopes, err)
				read, err := store.FindHolderScopesByKeyRange(ctx, "scope:none", cve, ranges)
				check("key-range", read.Scopes, err)
				masses, err := store.EstimateHolderMasses(ctx, cve)
				if err != nil {
					fail("mass read failed (deadlock=%v): %v", contentionFatal(err), err)
				} else if masses[replaceSetLiveCVE] < 2 {
					fail("mass of the CVE = %d, want at least the two always-live holders", masses[replaceSetLiveCVE])
				}
			}
		}()
	}
	const iterations = 25
	for _, scoped := range []bool{false, true} {
		writers.Add(1)
		go func() { // one writer per scope keeps its fencing tokens ordered
			defer writers.Done()
			for i := 0; i < iterations; i++ {
				findings := []supplychaincore.SupplyChainImpactFinding{
					replaceSetFinding(fmt.Sprintf("repository:r_11197_%v_%d", scoped, i)),
					replaceSetFinding(fmt.Sprintf("repository:r_11197_%v_%d", scoped, i+1)),
				}
				write := replaceSetWrite(fmt.Sprintf("intent:11197:contention-%v-%d", scoped, i), findings...)
				if scoped {
					write.ScopeID, write.GenerationID = replaceSetSecondScope, replaceSetSecondGeneration
				}
				if _, err := writer.WriteSupplyChainImpactFindings(ctx, write); err != nil {
					fail("writer %v pass %d: %v", scoped, i, err)
				}
			}
		}()
	}
	writers.Add(1)
	go func() { // a scope that is written and then cascade-deleted, repeatedly
		defer writers.Done()
		for i := 0; i < iterations/2; i++ {
			if err := contentionChurn(ctx, db, writer, i); err != nil {
				fail("churn %d: %v", i, err)
				return
			}
		}
	}()

	writers.Wait()
	stop.Store(true)
	readers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	if t.Failed() {
		return
	}
	if reads.Load() == 0 {
		t.Fatal("no reader completed a read")
	}

	// Quiesced: every read equals the retired fact oracle and the supports mirror
	// the live facts.
	want := queryScopes(t, ctx, db, oracleHoldersQuery, cve, "scope:none")
	got, err := store.FindHolderScopes(ctx, "scope:none", cve)
	if err != nil || !sameStrings(got, want) {
		t.Fatalf("holders after the load = %v, %v, want the fact oracle's %v", got, err, want)
	}
	read, err := store.FindHolderScopesByKeyRange(ctx, "scope:none", cve, ranges)
	if err != nil || !sameStrings(read.Scopes, want) {
		t.Fatalf("key-range after the load = %v, %v, want the fact oracle's %v", read.Scopes, err, want)
	}
	for _, index := range []string{holderMembersIdx, holderTokensIdx} {
		var pages int
		if err := db.QueryRowContext(ctx, `SELECT coalesce((SELECT n_pending_pages FROM gin_metapage_info(get_raw_page($1, 0))), -1)`, index).Scan(&pages); err == nil && pages != 0 {
			t.Fatalf("%s holds %d pending pages after the load, want 0 under fastupdate = off", index, pages)
		}
	}
	supportsLiveAssertMirror(t, ctx, db, replaceSetLiveScope, replaceSetLiveGeneration, "after the contention load")
	supportsLiveAssertMirror(t, ctx, db, replaceSetSecondScope, replaceSetSecondGeneration, "after the contention load (second scope)")
	var orphans int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM supply_chain_impact_finding_supports AS s
WHERE s.scope_id = $1`, contentionChurnScope).Scan(&orphans); err != nil || orphans != 0 {
		t.Fatalf("supports of the deleted churn scope = %d, %v, want 0 (cascade)", orphans, err)
	}
}

// contentionChurn creates the churn scope, writes a finding set into it, then
// removes the generation and the scope so the supports go by cascade.
func contentionChurn(ctx context.Context, db *sql.DB, writer supplychaincore.PostgresSupplyChainImpactWriter, i int) error {
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	steps := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'vulnerability_intelligence', 'synthetic', $1, 'synthetic', $1, $2, $2, 'active', '{}'::jsonb)`, []any{contentionChurnScope, now}},
		{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload)
VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`, []any{contentionChurnGeneration, contentionChurnScope, now}},
		{`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`, []any{contentionChurnGeneration, contentionChurnScope}},
	}
	for _, step := range steps {
		if _, err := db.ExecContext(ctx, step.query, step.args...); err != nil {
			return fmt.Errorf("seed churn scope: %w", err)
		}
	}
	write := replaceSetWrite(fmt.Sprintf("intent:11197:churn-%d", i),
		replaceSetFinding(fmt.Sprintf("repository:r_11197_churn_%d", i)))
	write.ScopeID, write.GenerationID = contentionChurnScope, contentionChurnGeneration
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, write); err != nil {
		var classified interface{ FailureClass() string }
		if !errors.As(err, &classified) || classified.FailureClass() != supplychaincore.SupplyChainImpactWriteSupersededFailureClass {
			return fmt.Errorf("write churn findings: %w", err)
		}
	}
	for _, q := range []string{
		`UPDATE ingestion_scopes SET active_generation_id = NULL WHERE scope_id = $1`,
		`DELETE FROM fact_records WHERE scope_id = $1 AND fact_kind = '` + facts.ReducerSupplyChainImpactFindingFactKind + `'`,
		`DELETE FROM scope_generations WHERE scope_id = $1`,
		`DELETE FROM ingestion_scopes WHERE scope_id = $1`,
	} {
		if _, err := db.ExecContext(ctx, q, contentionChurnScope); err != nil {
			return fmt.Errorf("remove churn scope: %w", err)
		}
	}
	return nil
}
