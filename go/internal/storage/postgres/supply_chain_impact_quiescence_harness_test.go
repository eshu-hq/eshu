// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// The quiescence harness backs the #10972 requeue-budget proofs
// (supply_chain_impact_quiescence_live_test.go). It adds three things to the
// convergence harness: pass-mode attribution from the real pass counter, the
// seeded violation the gate must catch, and the row, item and staging
// snapshots the budget compares. The queue, the flag, the ack trigger and the
// fan-out port are all production code.

// quiescePassMeter reads the pass_total counter by mode, so a sequential drain
// can attribute each pass's mode to its scope from the counter delta.
type quiescePassMeter struct {
	reader *sdkmetric.ManualReader
}

func (c *convergence) instrumentPassModes() quiescePassMeter {
	c.t.Helper()
	reader := sdkmetric.NewManualReader()
	inst, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("quiescence"))
	if err != nil {
		c.t.Fatalf("instruments: %v", err)
	}
	c.handler.Instruments = inst
	return quiescePassMeter{reader: reader}
}

func (m quiescePassMeter) totals(ctx context.Context) (map[string]int64, error) {
	var rm metricdata.ResourceMetrics
	if err := m.reader.Collect(ctx, &rm); err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if metric.Name != "eshu_dp_supply_chain_impact_pass_total" {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				mode, _ := dp.Attributes.Value(attribute.Key("mode"))
				out[mode.AsString()] += dp.Value
			}
		}
	}
	return out, nil
}

// quiesceModeIsFull reports whether a pass mode re-derived the whole finding
// set: the full mode and every fallback that ran full.
func quiesceModeIsFull(mode string) bool {
	return mode == "full" || strings.HasPrefix(mode, "scoped_fallback")
}

// quiescePass is one execution the budget counts.
type quiescePass struct {
	scope string
	mode  string
}

// handleCounted runs one claimed pass to its ack and names its mode from the
// counter delta. The caller must not run another pass concurrently.
func (c *convergence) handleCounted(meter quiescePassMeter, intent reducercontract.Intent) quiescePass {
	c.t.Helper()
	before, err := meter.totals(c.ctx)
	if err != nil {
		c.t.Fatalf("collect: %v", err)
	}
	result, err := c.handler.Handle(c.ctx, intent)
	if err != nil {
		c.t.Fatalf("handle %s: %v", intent.ScopeID, err)
	}
	if err := c.queue.Ack(c.ctx, intent, result); err != nil {
		c.t.Fatalf("ack %s: %v", intent.ScopeID, err)
	}
	after, err := meter.totals(c.ctx)
	if err != nil {
		c.t.Fatalf("collect: %v", err)
	}
	pass := quiescePass{scope: intent.ScopeID, mode: "none"}
	for mode, n := range after {
		if n > before[mode] {
			pass.mode = mode
		}
	}
	return pass
}

// drainCounted plays one sequential worker until nothing is claimable and
// returns every pass it ran. The limit only stops a runaway loop.
func (c *convergence) drainCounted(meter quiescePassMeter, limit int) []quiescePass {
	c.t.Helper()
	var passes []quiescePass
	for len(passes) < limit {
		intent, ok, err := c.queue.Claim(c.ctx)
		if err != nil {
			c.t.Fatalf("claim: %v", err)
		}
		if !ok {
			return passes
		}
		passes = append(passes, c.handleCounted(meter, intent))
	}
	return passes
}

// openSupplyChainItems counts supply_chain_impact items that have not settled.
func (c *convergence) openSupplyChainItems() int {
	c.t.Helper()
	var open int
	if err := c.db.QueryRowContext(c.ctx, `
SELECT count(*) FROM fact_work_items WHERE domain = 'supply_chain_impact' AND status <> 'succeeded'`).Scan(&open); err != nil {
		c.t.Fatalf("count open items: %v", err)
	}
	return open
}

// queryStringPairs reads a two-text-column result into a map.
func (c *convergence) queryStringPairs(query string, args ...any) map[string]string {
	c.t.Helper()
	rows, err := c.db.QueryContext(c.ctx, query, args...)
	if err != nil {
		c.t.Fatalf("query: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			c.t.Fatal(err)
		}
		out[key] = value
	}
	if err := rows.Err(); err != nil {
		c.t.Fatal(err)
	}
	return out
}

// findingXmins returns the xmin of every finding row the scopes hold, keyed by
// fact id. A pass that rewrites an unchanged row gives it a new xmin.
func (c *convergence) findingXmins(scopes []string) map[string]string {
	c.t.Helper()
	return c.queryStringPairs(`
SELECT fact_id, xmin::text FROM fact_records WHERE scope_id = ANY($1) AND fact_kind = $2`,
		scopes, facts.ReducerSupplyChainImpactFindingFactKind)
}

// reopenedAts returns every supply_chain_impact item's reopened_at, keyed by
// work item id ("" when never reopened).
func (c *convergence) reopenedAts() map[string]string {
	c.t.Helper()
	return c.queryStringPairs(`
SELECT work_item_id, COALESCE(reopened_at::text, '') FROM fact_work_items WHERE domain = 'supply_chain_impact'`)
}

// stagingWatermark returns the newest winners staging sequence.
func (c *convergence) stagingWatermark() int64 {
	c.t.Helper()
	var seq int64
	if err := c.db.QueryRowContext(c.ctx, `SELECT COALESCE(max(seq), 0) FROM supply_chain_impact_winners_dirty_staging`).Scan(&seq); err != nil {
		c.t.Fatalf("read staging watermark: %v", err)
	}
	return seq
}

// stagedFactMarks counts the impact writer's fact marks staged after a
// watermark. Activation rows are the collector's pointer flip, not the pass's.
func (c *convergence) stagedFactMarks(after int64) int {
	c.t.Helper()
	var n int
	if err := c.db.QueryRowContext(c.ctx, `
SELECT count(*) FROM supply_chain_impact_winners_dirty_staging
WHERE seq > $1 AND kind = 'fact' AND source IN ('fact_upsert', 'fact_retract')`, after).Scan(&n); err != nil {
		c.t.Fatalf("count staged marks: %v", err)
	}
	return n
}

// changedKeys lists the keys whose value differs between two snapshots.
func changedKeys(before, after map[string]string) []string {
	var out []string
	for key, value := range before {
		if got, ok := after[key]; !ok || got != value {
			out = append(out, key)
		}
	}
	for key := range after {
		if _, ok := before[key]; !ok {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// seedQuiesceEPSSHolder adds a scope whose only fact is an EPSS score for the
// convergence CVE, so its pass writes a copy of that CVE's finding.
func (c *convergence) seedQuiesceEPSSHolder(scope string) {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, scope, "g1")
	c.upsert(c.envelope(scope, fanOutGen(scope, "g1"), "fact:"+scope+":epss", chain.VulnerabilityEPSSScoreFactKind, map[string]any{
		"cve_id": convergenceCVE, "probability": "0.5", "percentile": "0.9", "score_date": "2026-10-31",
	}))
	fanOutSeedItem(c.t, c.ctx, c.db, scope, "g1")
}

// seedQuiesceUnrelatedScope adds a scope holding its own cves advisories, each
// with an affected package the scope consumes, and none of the convergence CVE.
func (c *convergence) seedQuiesceUnrelatedScope(scope string, cves []string) {
	c.t.Helper()
	fanOutSeedScope(c.t, c.ctx, c.db, scope, "g1")
	gen := fanOutGen(scope, "g1")
	var envelopes []facts.Envelope
	for j, cve := range cves {
		name := fmt.Sprintf("quiesce-10972-%s-%d", strings.ReplaceAll(scope, ":", "-"), j)
		pkg := "pkg:npm/" + name + "@1.0.0"
		id := func(part string) string { return fmt.Sprintf("fact:%s:%d:%s", scope, j, part) }
		envelopes = append(envelopes,
			c.envelope(scope, gen, id("cve"), chain.VulnerabilityCVEFactKind, map[string]any{
				"advisory_id": cve, "cve_id": cve, "source": "nvd", "cvss_score": 7.5, "status": "Analyzed",
			}),
			c.envelope(scope, gen, id("affected"), chain.VulnerabilityAffectedPackageFactKind, map[string]any{
				"advisory_id": cve, "cve_id": cve, "package_id": pkg, "ecosystem": "npm",
				"package_name": name, "affected_versions": []string{"1.0.0"}, "fixed_versions": []string{"1.1.0"},
			}),
			c.envelope(scope, gen, id("consumption"), factschema.FactKindReducerPackageConsumptionCorrelation, map[string]any{
				"package_id": pkg, "relationship_kind": "consumption", "repository_id": "repository:r_" + name,
				"dependency_range": "1.0.0", "canonical_writes": 1, "evidence_fact_ids": []string{id("manifest")},
			}),
		)
	}
	c.upsert(envelopes...)
	fanOutSeedItem(c.t, c.ctx, c.db, scope, "g1")
}

// stampSetupItem makes a scope's pending first item a one-hop pass for the
// convergence CVE. Setup only: N holders' genuine first passes would each
// reopen every holder settled before them (N squared passes); a one-hop first
// pass writes the same copy and reopens nobody.
func (c *convergence) stampSetupItem(scope string) {
	c.t.Helper()
	if _, err := c.db.ExecContext(c.ctx, `
UPDATE fact_work_items SET payload = payload || jsonb_build_object($2::text, jsonb_build_object(
  'scope_id', $3::text, 'generation_id', $4::text, 'intent_id', 'quiescence-setup', $5::text, jsonb_build_array($6::text)))
WHERE scope_id = $1 AND domain = 'supply_chain_impact' AND status = 'pending'`,
		scope, supplychaincore.PayloadKeyFanOutOrigin, convergenceSource, fanOutGen(convergenceSource, "g1"),
		supplychaincore.FanOutStampCVEKey, convergenceCVE); err != nil {
		c.t.Fatalf("stamp setup item %s: %v", scope, err)
	}
}

// statusOnlyInFlightFanOut is the seeded violation of the requeue budget: the
// in-flight flag as it stood before any evidence narrowed it. Set B is its own
// status-only statement (every claimed or running non-marker findings item at
// an active generation), written out on purpose, not derived from the shipped
// read, so a later narrowing of the shipped read cannot hide it. Set A, the
// load-end snapshot of #10972 D4b, always fails, so the handler flags every
// claim in B. Its reopen of a scope B returned carries no changed-id set, so the
// flag names no change and the rerun fails closed to a full pass. Holders the
// row read found keep the shipped stamp.
type statusOnlyInFlightFanOut struct {
	supplychaincore.SupplyChainImpactFanOut
	db       *sql.DB
	mu       sync.Mutex
	inFlight map[string]struct{}
}

// errSeededNoLoadEndRead makes the seeded port's set A read fail, which the
// D4b narrowing answers by flagging every claim in set B.
var errSeededNoLoadEndRead = errors.New("seeded status-only in-flight flag: no load-end read")

var statusOnlyInFlightQuery = `
SELECT work.scope_id, work.last_attempt_at
FROM fact_work_items AS work
JOIN ingestion_scopes AS scope
  ON scope.scope_id = work.scope_id AND scope.active_generation_id = work.generation_id
WHERE work.stage = 'reducer' AND work.domain = 'supply_chain_impact'
  AND work.status IN ('claimed', 'running') AND work.scope_id <> $1
  AND COALESCE(work.payload->>'` + supplychaincore.PayloadKeyTrigger + `', '') <> '` + supplychaincore.TriggerManifestDependency + `'
ORDER BY work.scope_id`

func (p *statusOnlyInFlightFanOut) FindInFlightNonMarkerClaims(ctx context.Context, own string) ([]supplychaincore.InFlightClaim, error) {
	rows, err := p.db.QueryContext(ctx, statusOnlyInFlightQuery, own)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []supplychaincore.InFlightClaim
	for rows.Next() {
		var (
			scope     string
			claimedAt sql.NullTime
		)
		if err := rows.Scan(&scope, &claimedAt); err != nil {
			return nil, err
		}
		out = append(out, supplychaincore.InFlightClaim{ScopeID: scope, ClaimedAt: claimedAt.Time})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inFlight == nil {
		p.inFlight = map[string]struct{}{}
	}
	for _, claim := range out {
		p.inFlight[claim.ScopeID] = struct{}{}
	}
	return out, nil
}

func (p *statusOnlyInFlightFanOut) SnapshotInFlightNonMarkerClaims(context.Context, string) ([]supplychaincore.InFlightClaim, error) {
	return nil, errSeededNoLoadEndRead
}

func (p *statusOnlyInFlightFanOut) ReopenHolderDetailed(ctx context.Context, holder string, origin supplychaincore.FanOutOrigin) (supplychaincore.FanOutReopen, error) {
	p.mu.Lock()
	_, flagged := p.inFlight[holder]
	p.mu.Unlock()
	if flagged {
		origin.CVEIDs = nil
	}
	return p.SupplyChainImpactFanOut.ReopenHolderDetailed(ctx, holder, origin)
}

// quiesceLoadEndHookFanOut runs hook once, right after the given scope's load-end
// in-flight read (set A of #10972 D4b) returns and before its fan-out reads
// set B, so a test can claim passes in exactly the window D4b narrows.
type quiesceLoadEndHookFanOut struct {
	supplychaincore.SupplyChainImpactFanOut
	scope string
	hook  func()
	once  sync.Once
}

func (p *quiesceLoadEndHookFanOut) SnapshotInFlightNonMarkerClaims(ctx context.Context, own string) ([]supplychaincore.InFlightClaim, error) {
	claims, err := p.SupplyChainImpactFanOut.SnapshotInFlightNonMarkerClaims(ctx, own)
	if own == p.scope {
		p.once.Do(p.hook)
	}
	return claims, err
}
