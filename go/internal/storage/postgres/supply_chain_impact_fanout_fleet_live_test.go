// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// The fleet proof measures what the in-flight read costs a burst. N scopes each
// hold a DISTINCT CVE, so the row read finds no holder and the in-flight read is
// the only fan-out path. All N have a status change queued at once and four
// workers drain them with a padded pass, with the in-flight read on and then off.
const (
	fleetScopes  = 24
	fleetWorkers = 4
	fleetPassPad = 400 * time.Millisecond
	// fleetRatioCeiling is the arbiter's bound: more than this many passes per
	// genuine pass means the in-flight read needs the narrowing in this PR.
	fleetRatioCeiling = 2.5
)

// fleetNoInFlight is the port with the in-flight read removed: the "off" arm.
type fleetNoInFlight struct {
	supplychaincore.SupplyChainImpactFanOut
}

func (fleetNoInFlight) FindInFlightScopes(context.Context, string) ([]string, error) {
	return nil, nil
}

// fleetPaddedWriter makes a pass take about fleetPassPad between its evidence
// load and its commit, like a pass that does real work.
type fleetPaddedWriter struct {
	inner supplychaincore.SupplyChainImpactWriter
}

func (w fleetPaddedWriter) WriteSupplyChainImpactFindings(ctx context.Context, in supplychaincore.SupplyChainImpactWrite) (supplychaincore.SupplyChainImpactWriteResult, error) {
	time.Sleep(fleetPassPad)
	return w.inner.WriteSupplyChainImpactFindings(ctx, in)
}

type fleetResult struct {
	passes, flagged, reopened int64
	makespan                  time.Duration
}

func fleetScope(i int) string { return fmt.Sprintf("scope:9695:fleet:%02d", i) }

func (c *convergence) seedFleetScope(i int) {
	c.t.Helper()
	scope, gen := fleetScope(i), fanOutGen(fleetScope(i), "g1")
	cve, pkg := fmt.Sprintf("CVE-2026-95%02d", i), fmt.Sprintf("pkg:npm/fleet-9695-%02d@1.0.0", i)
	id := func(part string) string { return fmt.Sprintf("fact:9695:fleet:%02d:%s", i, part) }
	fanOutSeedScope(c.t, c.ctx, c.db, scope, "g1")
	c.upsert(
		c.envelope(scope, gen, id("cve"), chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": cve, "cve_id": cve, "source": "nvd", "cvss_score": 7.5, "status": "Analyzed",
		}),
		c.envelope(scope, gen, id("affected"), chain.VulnerabilityAffectedPackageFactKind, map[string]any{
			"advisory_id": cve, "cve_id": cve, "package_id": pkg, "ecosystem": "npm",
			"package_name": fmt.Sprintf("fleet-9695-%02d", i), "affected_versions": []string{"1.0.0"}, "fixed_versions": []string{"1.1.0"},
		}),
		c.envelope(scope, gen, id("consumption"), factschema.FactKindReducerPackageConsumptionCorrelation, map[string]any{
			"package_id": pkg, "relationship_kind": "consumption", "repository_id": fmt.Sprintf("repository:r_9695_fleet_%02d", i),
			"dependency_range": "1.0.0", "canonical_writes": 1, "evidence_fact_ids": []string{id("manifest")},
		}),
	)
	fanOutSeedItem(c.t, c.ctx, c.db, scope, "g1")
}

// fleetBurst seeds the fleet, settles every scope's first pass one at a time,
// changes every record's status, queues all N passes at once, and drains them with
// concurrent workers until nothing is left. It returns what the burst cost.
func fleetBurst(t *testing.T, inFlight bool) fleetResult {
	t.Helper()
	c := newConvergence(t, false)
	for i := 0; i < fleetScopes; i++ {
		c.seedFleetScope(i)
	}
	c.drain() // first pass of every scope, one at a time: nothing overlaps
	if _, err := c.db.ExecContext(c.ctx, `
UPDATE fact_records SET payload = jsonb_set(payload, '{status}', '"Rejected"')
WHERE fact_id LIKE 'fact:9695:fleet:%:cve'`); err != nil {
		t.Fatal(err)
	}
	c.handler.Writer = fleetPaddedWriter{inner: c.handler.Writer}
	if !inFlight {
		c.handler.FanOut = fleetNoInFlight{c.fanOut}
	}
	for i := 0; i < fleetScopes; i++ {
		c.rerun(fleetScope(i))
	}
	var passes, flagged, reopened atomic.Int64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	start := time.Now()
	for w := 0; w < fleetWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				intent, ok, err := c.queue.Claim(c.ctx)
				if err != nil {
					t.Errorf("claim: %v", err)
					return
				}
				if !ok {
					time.Sleep(10 * time.Millisecond)
					continue
				}
				result, herr := c.handler.Handle(c.ctx, intent)
				if herr != nil {
					t.Errorf("handle %s: %v", intent.ScopeID, herr)
					_ = c.queue.Fail(c.ctx, intent, herr)
					continue
				}
				if strings.HasPrefix(intent.ScopeID, "scope:9695:fleet:") {
					passes.Add(1)
					flagged.Add(int64(result.SubSignals["fanout_replay_flagged"]))
					reopened.Add(int64(result.SubSignals["fanout_reopened"]))
				}
				if aerr := c.queue.Ack(c.ctx, intent, result); aerr != nil {
					t.Errorf("ack: %v", aerr)
				}
			}
		}()
	}
	quiet := 0
	for quiet < 3 && time.Since(start) < 120*time.Second {
		var open int
		if err := c.db.QueryRowContext(c.ctx, `
SELECT count(*) FROM fact_work_items WHERE domain = 'supply_chain_impact'
  AND scope_id LIKE 'scope:9695:fleet:%' AND status <> 'succeeded'`).Scan(&open); err != nil {
			t.Fatal(err)
		}
		if open == 0 {
			quiet++
		} else {
			quiet = 0
		}
		time.Sleep(40 * time.Millisecond)
	}
	makespan := time.Since(start)
	close(stop)
	wg.Wait()
	if quiet < 3 {
		t.Fatalf("fleet did not settle in %s", makespan)
	}
	fleetWantRetracted(t, c)
	return fleetResult{passes: passes.Load(), flagged: flagged.Load(), reopened: reopened.Load(), makespan: makespan}
}

// fleetWantRetracted checks every scope converged: the status change reached each
// scope's own copy, so the burst measured work that changed the answer.
func fleetWantRetracted(t *testing.T, c *convergence) {
	t.Helper()
	rows, err := c.db.QueryContext(c.ctx, `
SELECT f.scope_id, COALESCE(f.payload->>'suppression_state','') FROM fact_records f
JOIN ingestion_scopes s ON s.scope_id = f.scope_id AND s.active_generation_id = f.generation_id
WHERE f.fact_kind = $1 AND NOT f.is_tombstone AND f.scope_id LIKE 'scope:9695:fleet:%'`, facts.ReducerSupplyChainImpactFindingFactKind)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var bad []string
	seen := 0
	for rows.Next() {
		var scope, state string
		if err := rows.Scan(&scope, &state); err != nil {
			t.Fatal(err)
		}
		seen++
		if state != "advisory_retracted" {
			bad = append(bad, scope+"="+state)
		}
	}
	sort.Strings(bad)
	if seen != fleetScopes || len(bad) > 0 {
		t.Fatalf("fleet copies: %d of %d scopes have a finding, not retracted: %v", seen, fleetScopes, bad)
	}
}

// TestFanOutRaceFleetBurstCostOfTheInFlightReadLive is the fleet-cost proof the
// arbiter required. With the in-flight read off a burst of N genuine passes costs
// N passes (each scope holds a distinct CVE, so the row read reaches nothing).
// With it on, a changed commit flags every pass in flight, so about every genuine
// pass gets one one-hop rerun: about 2N passes. The test pins both arms and the
// ceiling: more than fleetRatioCeiling passes per genuine pass fails it.
func TestFanOutRaceFleetBurstCostOfTheInFlightReadLive(t *testing.T) {
	// Each arm opens its own disposable database, and a missing live DSN skips the
	// whole test before any result is read.
	off := fleetBurst(t, false)
	on := fleetBurst(t, true)
	for _, row := range []struct {
		name string
		r    fleetResult
	}{{"off", off}, {"on", on}} {
		t.Logf("FLEET in_flight_read=%s N=%d W=%d pass=%s passes=%d ratio=%.2f replay_flagged=%d reopened=%d makespan=%.2fs",
			row.name, fleetScopes, fleetWorkers, fleetPassPad, row.r.passes, float64(row.r.passes)/fleetScopes,
			row.r.flagged, row.r.reopened, row.r.makespan.Seconds())
	}
	if off.passes != fleetScopes || off.flagged != 0 || off.reopened != 0 {
		t.Fatalf("read off = %+v, want exactly %d passes and no flags: distinct CVEs leave the row read nothing to reach", off, fleetScopes)
	}
	// A holder that acknowledges between the in-flight read and the reopen statement
	// is reopened, not flagged; either way it costs exactly one rerun.
	if on.passes != fleetScopes+on.flagged+on.reopened {
		t.Fatalf("read on = %+v, want passes = N + replay_flagged + reopened: each flag or reopen is exactly one rerun", on)
	}
	if ratio := float64(on.passes) / fleetScopes; ratio > fleetRatioCeiling {
		t.Fatalf("read on cost %.2fx genuine passes (%+v), above the %.1fx ceiling: build the narrowing", ratio, on, fleetRatioCeiling)
	}
	if float64(on.passes) < 1.5*fleetScopes {
		t.Fatalf("read on cost %d passes for %d scopes (%+v): the in-flight read flagged almost nothing, so the arm measured nothing", on.passes, fleetScopes, on)
	}
}
