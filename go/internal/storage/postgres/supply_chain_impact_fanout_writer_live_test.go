// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestMutualFanOutHasNoDeadlockLive runs concurrent sessions that reopen the same
// holders in opposite orders. Each statement holds one row lock, so no cycle can
// form; the proof is that every statement finishes without a deadlock error and
// every holder ends pending.
func TestMutualFanOutHasNoDeadlockLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	const holders = 10
	names := make([]string, 0, holders)
	for i := 0; i < holders; i++ {
		scope := fmt.Sprintf("holder-%02d", i)
		fanOutSeedScope(t, ctx, db, scope, "g1")
		fanOutSeedItem(t, ctx, db, scope, "g1")
		names = append(names, scope)
	}
	store := fanOutLiveStore(db)
	for round := 0; round < 30; round++ {
		if _, err := db.ExecContext(ctx, `UPDATE fact_work_items SET status='succeeded', lease_owner=NULL, claim_until=NULL, visible_at=NULL WHERE domain='supply_chain_impact'`); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		errs := make(chan error, 3*holders)
		for session := 0; session < 3; session++ {
			wg.Add(1)
			go func(reverse bool) {
				defer wg.Done()
				order := append([]string(nil), names...)
				if reverse {
					sort.Sort(sort.Reverse(sort.StringSlice(order)))
				}
				for _, holder := range order {
					if _, err := store.ReopenHolder(ctx, holder, fanOutLiveOrigin); err != nil {
						errs <- err
					}
				}
			}(session == 1)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("round %d: %v", round, err)
		}
		var notPending int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fact_work_items WHERE domain='supply_chain_impact' AND status <> 'pending'`).Scan(&notPending); err != nil || notPending != 0 {
			t.Fatalf("round %d: %d holders not pending (err=%v)", round, notPending, err)
		}
	}
}

func fanOutLiveFinding(cve string) supplychaincore.SupplyChainImpactFinding {
	return supplychaincore.SupplyChainImpactFinding{
		CVEID: cve, PackageID: "pkg:npm/" + strings.ToLower(cve), Ecosystem: "npm", ObservedVersion: "1.0.0",
		Status: supplychaincore.SupplyChainImpactAffectedExact, RepositoryID: "repository:r_9695",
	}
}

// TestWriterReportsChangesAgainstThePreviousGenerationLive runs the real writer
// over real rows. A finding's fact id embeds its generation, so the first pass of
// a new generation is judged against the scope's previous generation by stable
// key: an identical rescan changes nothing, a finding the rescan lacks and one it
// adds are changed, a rerun is judged against the generation's own rows, and a
// recovery write names the CVEs of both generations.
func TestWriterReportsChangesAgainstThePreviousGenerationLive(t *testing.T) {
	ctx, db := fanOutLiveDB(t)
	fanOutSeedScope(t, ctx, db, "source", "g2", "g1")
	writer := supplychaincore.PostgresSupplyChainImpactWriter{
		DB: postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
	}
	token := int64(0)
	write := func(generation string, recovery bool, cves ...string) supplychaincore.SupplyChainImpactWriteResult {
		t.Helper()
		token++
		findings := make([]supplychaincore.SupplyChainImpactFinding, 0, len(cves))
		for _, cve := range cves {
			findings = append(findings, fanOutLiveFinding(cve))
		}
		result, err := writer.WriteSupplyChainImpactFindings(ctx, supplychaincore.SupplyChainImpactWrite{
			IntentID: fmt.Sprintf("intent-%d", token), ScopeID: "source", GenerationID: fanOutGen("source", generation),
			SourceSystem: "synthetic", Cause: fmt.Sprintf("pass %d", token), Findings: findings, FencingToken: token, Recovery: recovery,
		})
		if err != nil {
			t.Fatalf("write %s: %v", generation, err)
		}
		return result
	}

	if got := write("g1", false, "CVE-A", "CVE-B").ChangedCVEIDs; !reflect.DeepEqual(got, []string{"CVE-A", "CVE-B"}) {
		t.Fatalf("first-ever pass changed = %v, want both CVEs", got)
	}
	if got := write("g1", false, "CVE-A", "CVE-B").ChangedCVEIDs; len(got) != 0 {
		t.Fatalf("identical rerun changed = %v, want none", got)
	}
	if got := write("g2", false, "CVE-A", "CVE-B").ChangedCVEIDs; len(got) != 0 {
		t.Fatalf("identical rescan changed = %v, want none: the previous generation is the baseline", got)
	}
	// A rescan that drops B and adds C. The previous generation is g2 only once
	// g2 is current, so write the g1 baseline again and then the changed g2.
	if _, err := db.ExecContext(ctx, `DELETE FROM fact_records WHERE scope_id = 'source' AND generation_id = $1`, fanOutGen("source", "g2")); err != nil {
		t.Fatal(err)
	}
	if got := write("g2", false, "CVE-A", "CVE-C").ChangedCVEIDs; !reflect.DeepEqual(got, []string{"CVE-B", "CVE-C"}) {
		t.Fatalf("changed rescan = %v, want the dropped CVE-B and the new CVE-C", got)
	}
	// A revived row: tombstone CVE-A's row, then derive it again. The read lists
	// active rows only, so the revived row looks new and counts as changed.
	if _, err := db.ExecContext(ctx, `UPDATE fact_records SET is_tombstone = TRUE WHERE scope_id = 'source' AND generation_id = $1 AND payload->>'cve_id' = 'CVE-A'`, fanOutGen("source", "g2")); err != nil {
		t.Fatal(err)
	}
	if got := write("g2", false, "CVE-A", "CVE-C").ChangedCVEIDs; !reflect.DeepEqual(got, []string{"CVE-A"}) {
		t.Fatalf("revived row changed = %v, want CVE-A: a tombstoned row is not a baseline", got)
	}
	again := write("g2", true, "CVE-A", "CVE-C")
	if len(again.ChangedCVEIDs) != 0 {
		t.Fatalf("rerun of the changed generation changed = %v, want none: it is judged against its own rows", again.ChangedCVEIDs)
	}
	if want := []string{"CVE-A", "CVE-B", "CVE-C"}; !reflect.DeepEqual(again.RecoveryCVEIDs, want) {
		t.Fatalf("recovery CVEs = %v, want %v: both generations' CVEs", again.RecoveryCVEIDs, want)
	}
}
