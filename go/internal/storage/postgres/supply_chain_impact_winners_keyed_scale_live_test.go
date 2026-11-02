// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The scale proof knob. Unset or "smoke" runs a corpus of two percent of 1x, so
// the harness stays a passing, fast gate test. "1x", "5x" and "10x" run the
// measured corpus; they belong on a quiet host (see the evidence note).
const (
	winnersScaleEnv        = "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_SCALE_PROOF"
	winnersScaleExplainEnv = "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_SCALE_EXPLAIN"
	winnersScaleReportEnv  = "ESHU_SUPPLY_CHAIN_IMPACT_WINNERS_SCALE_REPORT"
	winnersScaleScopeSize  = 1000
)

// winnersScaleCorpus is the generated corpus shape. 1x is 100,000 findings in
// 100 scopes of 1,000. Finding g maps to key g mod K with K = 5N/6, so a key has
// about 1.2 candidates in different scopes. Keys are grouped ten to a package
// triple, and one key in twenty sits on a single hot triple, which is the shape
// that makes the keyed probe fetch sibling keys from the heap.
type winnersScaleCorpus struct {
	label    string
	scopes   int
	findings int
	keys     int
}

func winnersScaleCorpusFor(t *testing.T, knob string) winnersScaleCorpus {
	t.Helper()
	multiple := map[string]float64{"": 0.02, "smoke": 0.02, "1x": 1, "5x": 5, "10x": 10}
	m, ok := multiple[knob]
	if !ok {
		t.Fatalf("%s=%q: want smoke, 1x, 5x or 10x", winnersScaleEnv, knob)
	}
	label := knob
	if label == "" {
		label = "smoke"
	}
	findings := int(100000 * m)
	return winnersScaleCorpus{label: label, scopes: findings / winnersScaleScopeSize, findings: findings, keys: findings * 5 / 6}
}

// finding returns global finding g with the priority offset applied.
func (c winnersScaleCorpus) finding(g, bump int) supplychaincore.SupplyChainImpactFinding {
	k := g % c.keys
	pkg, repo := fmt.Sprintf("pkg:npm/p%d", k/10), fmt.Sprintf("repo-%d", k/100)
	if k%20 == 0 {
		pkg, repo = "pkg:npm/hot", "repo-hot"
	}
	finding := keyedFinding(fmt.Sprintf("CVE-2026-%07d", k), pkg, (g*7)%100+bump)
	finding.RepositoryID = repo
	return finding
}

// findingsFrom returns n consecutive findings starting at global finding first,
// as one scope's write set.
func (c winnersScaleCorpus) findingsFrom(first, n int) []supplychaincore.SupplyChainImpactFinding {
	findings := make([]supplychaincore.SupplyChainImpactFinding, 0, n)
	for i := 0; i < n; i++ {
		findings = append(findings, c.finding(first+i, 0))
	}
	return findings
}

func (c winnersScaleCorpus) scopeID(s int) string {
	return fmt.Sprintf("vuln-intel://9805-scale/s%04d", s)
}

func (c winnersScaleCorpus) writeScope(h *keyedHarness, s, bump int) {
	findings := make([]supplychaincore.SupplyChainImpactFinding, 0, winnersScaleScopeSize)
	for i := 0; i < winnersScaleScopeSize; i++ {
		findings = append(findings, c.finding(s*winnersScaleScopeSize+i, bump))
	}
	h.write(c.scopeID(s), c.scopeID(s)+"/g1", findings...)
}

// hostLoad reports the one-minute load average of the host, or "unknown".
func hostLoad() string {
	out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
		return "unknown"
	}
	fields := strings.Fields(strings.Trim(string(out), "{} \n"))
	if len(fields) == 0 {
		return "unknown"
	}
	return fields[0]
}

// TestSupplyChainImpactWinnersResweepScaleProofLive is the retained scale and
// parity harness of #9805. It builds the corpus through the production impact
// writer, runs the full resweep, rewrites one percent of the findings through the
// writer, then times one keyed tick and one full resweep over the same state and
// requires the keyed table to equal the full recompute. With the explain knob it
// also attaches the EXPLAIN (ANALYZE, BUFFERS) plans of the tick's active_facts
// build and apply statement. The smoke corpus keeps it in the gate; the report
// lines carry the host load so a figure is never read without it.
func TestSupplyChainImpactWinnersResweepScaleProofLive(t *testing.T) {
	corpus := winnersScaleCorpusFor(t, winnersScaleKnob(winnersScaleEnv))
	h := newKeyedHarness(t, "#9805 scale proof")
	if corpus.label != "smoke" {
		// The harness context allows five minutes, which a measured corpus load
		// alone can exceed.
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
		t.Cleanup(cancel)
		h.ctx = ctx
		// Measured scales run with the production caps. The smoke corpus is two
		// scopes, so any churn exceeds half of it and would take the full path.
		h.store.MaxKeyFraction = 0
		h.store.MaxIdentifiers = 0
	}
	loadBefore := hostLoad()

	loadStart := time.Now()
	for s := 0; s < corpus.scopes; s++ {
		h.seedScope(corpus.scopeID(s), corpus.scopeID(s)+"/g1")
		corpus.writeScope(h, s, 0)
	}
	loadElapsed := time.Since(loadStart)
	h.exec(`DELETE FROM supply_chain_impact_winners_dirty_log`)
	h.exec(`ANALYZE fact_records`)

	fullStart := time.Now()
	initial, err := h.store.ResweepWinners(h.ctx, h.tick())
	initialFull := time.Since(fullStart)
	if err != nil || initial.Skipped {
		t.Fatalf("initial full resweep = %+v, %v", initial, err)
	}
	winners := h.count(`SELECT count(*) FROM supply_chain_impact_canonical_winners`)

	// 1% churn: rewrite every (scopes/churnScopes)th scope with new priorities.
	churnScopes := max(1, corpus.scopes/100)
	churnStart := time.Now()
	for i := 0; i < churnScopes; i++ {
		corpus.writeScope(h, i*(corpus.scopes/churnScopes), 1000)
	}
	churnElapsed := time.Since(churnStart)
	pending := h.dirtyDepth()

	var activePlan, applyPlan string
	if winnersScaleKnob(winnersScaleExplainEnv) == "1" {
		if activePlan, applyPlan, err = postgres.ExplainWinnersKeyedForTest(h.ctx, h.db, h.tick()); err != nil {
			t.Fatalf("explain: %v", err)
		}
	}

	keyedStart := time.Now()
	keyed := h.keyed()
	keyedElapsed := time.Since(keyedStart)
	snapshot := winnersLiveRead(t, h.ctx, h.db)

	fullStart = time.Now()
	again, err := h.store.ResweepWinners(h.ctx, h.tick())
	fullElapsed := time.Since(fullStart)
	if err != nil || again.Skipped {
		t.Fatalf("full resweep over the churned state = %+v, %v", again, err)
	}
	if again.Updated != 0 || again.Inserted != 0 || again.Deleted != 0 {
		t.Errorf("a full resweep after the keyed tick wrote inserted=%d updated=%d deleted=%d: the keyed table was not equal to a recompute",
			again.Inserted, again.Updated, again.Deleted)
	}
	fresh := winnersLiveRead(t, h.ctx, h.db)
	if len(fresh) != len(snapshot) {
		t.Fatalf("parity: fresh recompute has %d winners, keyed table had %d", len(fresh), len(snapshot))
	}
	for key, row := range snapshot {
		if got, ok := fresh[key]; !ok || got.content != row.content {
			t.Errorf("parity: winner %s differs from a fresh recompute", key)
		}
	}
	if keyed.Path != "keyed" {
		t.Errorf("keyed tick ran on path %q at %s: the cap sent it to the full rebuild", keyed.Path, corpus.label)
	}

	ratio := float64(fullElapsed) / float64(keyedElapsed)
	report := strings.Join([]string{
		fmt.Sprintf("scale=%s findings=%d scopes=%d keys=%d winners=%d host_load1_before=%s host_load1_after=%s",
			corpus.label, corpus.findings, corpus.scopes, corpus.keys, winners, loadBefore, hostLoad()),
		fmt.Sprintf("corpus_load=%s initial_full=%s churn_write=%s churned_scopes=%d dirty_rows=%d", loadElapsed, initialFull, churnElapsed, churnScopes, pending),
		fmt.Sprintf("keyed_tick=%s path=%s identifiers=%d keys=%d updated=%d unchanged=%d",
			keyedElapsed, keyed.Path, keyed.Identifiers, keyed.Keys, keyed.Updated, keyed.Unchanged),
		fmt.Sprintf("full_resweep_same_state=%s ratio_full_over_keyed=%.1fx", fullElapsed, ratio),
		fmt.Sprintf("per_winner_full=%.1fus per_identifier_keyed=%.1fus",
			float64(fullElapsed.Microseconds())/float64(max(winners, 1)), float64(keyedElapsed.Microseconds())/float64(max(int(keyed.Identifiers), 1))),
	}, "\n")
	t.Log("\n" + report)
	if rounds := winnersWriterCostRounds(t); rounds > 0 {
		report += "\n" + runWinnersWriterCost(h, corpus, rounds)
		t.Log("\n" + report)
	}
	if n := winnersPurgeCostFindings(t); n > 0 {
		report += "\n" + runWinnersPurgeCost(h, corpus, n)
		t.Log("\n" + report)
	}
	if path := winnersScaleKnob(winnersScaleReportEnv); path != "" {
		body := report + "\n"
		if activePlan != "" {
			body += "\n--- EXPLAIN (ANALYZE, BUFFERS) winners_keyed_active_facts build ---\n" + activePlan + "\n\n--- EXPLAIN (ANALYZE, BUFFERS) applyWinnersKeyedSQL ---\n" + applyPlan + "\n"
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write report: %v", err)
		}
	}
}
