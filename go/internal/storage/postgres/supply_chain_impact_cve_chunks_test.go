// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
)

const cveChunkBranch = "vulnerability_cve_id"

// cveChunkIDs returns n distinct well-formed ids in ascending byte order.
func cveChunkIDs(n int) []string {
	ids := make([]string, 0, n)
	for i := range n {
		ids = append(ids, fmt.Sprintf("CVE-2024-%05d", i))
	}
	return ids
}

// cveChunkOrderings returns the same ids in several orders, with and without
// duplicates. The loader normalizes the list, so the statements it issues must
// not depend on the order a caller collected the ids in.
func cveChunkOrderings(ids []string) map[string][]string {
	reversed := slices.Clone(ids)
	slices.Reverse(reversed)
	shuffled := slices.Clone(ids)
	rand.New(rand.NewSource(9994)).Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
	rotated := append(slices.Clone(ids[len(ids)/2:]), ids[:len(ids)/2]...)
	return map[string][]string{
		"ascending":  ids,
		"descending": reversed,
		"shuffled":   shuffled,
		"rotated":    rotated,
		"duplicated": append(slices.Clone(shuffled), ids...),
	}
}

// TestCVEListIsChunkedOnTheKeyProbe is the #9994 plan guard on the loader. The
// statement at 380 or more ids must become several statements, each at or below the
// bound, that between them bind every id once. The bound itself stays out of the
// walk the planner picks at 172 to 199 ids and from 389.
func TestCVEListIsChunkedOnTheKeyProbe(t *testing.T) {
	t.Parallel()

	bound := factimpact.MaxCVEIDsPerStatement
	for _, size := range []int{1, bound - 1, bound, bound + 1, 180, 239, 240, 241, 380, 400, 1000, 2000} {
		for order, list := range cveChunkOrderings(cveChunkIDs(size)) {
			t.Run(fmt.Sprintf("%d/%s", size, order), func(t *testing.T) {
				t.Parallel()

				routed := &supplyChainImpactRoutingDB{branchRows: map[string][][2]string{}, facts: map[string]string{}}
				if _, stats, err := NewFactStore(routed).ListActiveSupplyChainImpactFactsWithStats(context.Background(),
					supplychaincore.SupplyChainImpactFactFilter{CVEIDs: list}); err != nil {
					t.Fatalf("ListActiveSupplyChainImpactFactsWithStats: %v", err)
				} else {
					statements := 0
					for _, branch := range stats.Branches {
						if branch.Branch == cveChunkBranch {
							statements++
						}
					}
					want := (size + bound - 1) / bound
					if statements != want {
						t.Errorf("branch stats = %d, want %d: one per statement, so telemetry can count them", statements, want)
					}
				}
				calls := routed.callsFor(cveChunkBranch)
				var issued []string
				for _, call := range calls {
					chunk := boundList(t, call, factimpact.CVEIDs)
					if len(chunk) == 0 || len(chunk) > bound {
						t.Errorf("one statement bound %d ids, want 1 to %d", len(chunk), bound)
					}
					issued = append(issued, chunk...)
				}
				if want := cveChunkIDs(size); !slices.Equal(issued, want) {
					t.Errorf("chunks bound %d ids, want each of the %d once and in byte order", len(issued), len(want))
				}
			})
		}
	}
}

// TestCVEChunksUnionEveryStatement answers each bound id with its own row, so a
// loader that kept only some statements' rows returns too few.
func TestCVEChunksUnionEveryStatement(t *testing.T) {
	t.Parallel()

	ids := cveChunkIDs(1000)
	facts := map[string]string{}
	for _, id := range ids {
		facts[id] = "vulnerability.cve"
	}
	routed := &supplyChainImpactRoutingDB{
		branchRowsPerEntry: map[string]string{cveChunkBranch: "vulnerability.cve"},
		facts:              facts,
	}
	loaded, _, err := NewFactStore(routed).ListActiveSupplyChainImpactFacts(context.Background(),
		supplychaincore.SupplyChainImpactFactFilter{CVEIDs: ids})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts: %v", err)
	}
	got := make([]string, 0, len(loaded))
	for _, envelope := range loaded {
		got = append(got, envelope.FactID)
	}
	slices.Sort(got)
	if !slices.Equal(got, ids) {
		t.Errorf("loaded %d facts, want each of the %d ids once", len(got), len(ids))
	}
}

// TestCVEChunksKeepOneSnapshotAndDedupe: the chunks run inside the one read-only
// repeatable-read snapshot of the load, and a row more than one statement returns
// still reaches the caller once.
func TestCVEChunksKeepOneSnapshotAndDedupe(t *testing.T) {
	t.Parallel()

	var begins int
	routed := &supplyChainImpactRoutingDB{
		branchRows: map[string][][2]string{cveChunkBranch: {{"cve-fact", "vulnerability.cve"}}},
		facts:      map[string]string{"cve-fact": "vulnerability.cve"},
	}
	loaded, _, err := NewFactStore(cveChunkBeginCounter{routed, &begins}).ListActiveSupplyChainImpactFacts(context.Background(),
		supplychaincore.SupplyChainImpactFactFilter{CVEIDs: cveChunkIDs(1000)})
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts: %v", err)
	}
	if begins != 1 {
		t.Errorf("snapshots opened = %d, want 1", begins)
	}
	if routed.commits != 1 {
		t.Errorf("commits = %d, want 1", routed.commits)
	}
	if len(routed.callsFor(cveChunkBranch)) < 2 {
		t.Fatalf("statements = %d, want several for 1,000 ids", len(routed.callsFor(cveChunkBranch)))
	}
	if len(loaded) != 1 || loaded[0].FactID != "cve-fact" {
		t.Errorf("loaded = %#v, want the one row once", loaded)
	}
}

// TestCVEChunkFailureFailsTheWholeLoad: a statement that fails part-way must not
// leave a partial load the caller could mistake for the whole evidence set. A
// retry reruns every chunk in a new snapshot, so a retry is idempotent.
func TestCVEChunkFailureFailsTheWholeLoad(t *testing.T) {
	t.Parallel()

	failure := errors.New("statement timeout")
	routed := &supplyChainImpactRoutingDB{branchRows: map[string][][2]string{}, facts: map[string]string{}}
	loaded, _, err := NewFactStore(cveChunkFailAfter{routed, 2, failure}).ListActiveSupplyChainImpactFacts(context.Background(),
		supplychaincore.SupplyChainImpactFactFilter{CVEIDs: cveChunkIDs(1000)})
	if !errors.Is(err, failure) {
		t.Fatalf("error = %v, want the statement failure", err)
	}
	if loaded != nil {
		t.Fatalf("loaded = %d rows, want none from a failed load", len(loaded))
	}
}

// TestCVEChunkingLeavesOtherCVEStatementsWhole: only the key probe is cut. The
// suppression statement carries a LIMIT and the alert and guard statements have
// no list-length plan flip to avoid, so each sees the whole list once.
func TestCVEChunkingLeavesOtherCVEStatementsWhole(t *testing.T) {
	t.Parallel()

	ids := cveChunkIDs(500)
	routed := &supplyChainImpactRoutingDB{branchRows: map[string][][2]string{}, facts: map[string]string{}}
	if _, _, err := NewFactStore(routed).ListActiveSupplyChainImpactFacts(context.Background(),
		supplychaincore.SupplyChainImpactFactFilter{CVEIDs: ids}); err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts: %v", err)
	}
	for _, label := range []string{"security_alert_cve_id", "vulnerability_suppression", "foreign_key_guard"} {
		calls := routed.callsFor(label)
		if len(calls) != 1 || !slices.Equal(boundList(t, calls[0], factimpact.CVEIDs), ids) {
			t.Errorf("%s statements = %d, want 1 with the whole list", label, len(calls))
		}
	}
}

// TestCVEChunkBranchRejectsNonListParam: a chunked branch bound to a non-list
// param must fail loudly. The ignored type assertion used to return zero rows,
// which the loader would mistake for complete evidence (missing vulnerability
// rows fail open as missed findings). Every chunked CVE key probe is held to
// it, including the #9914 upstream probe: one shared chunker, one rule.
func TestCVEChunkBranchRejectsNonListParam(t *testing.T) {
	t.Parallel()

	for _, name := range []string{cveChunkBranch, factimpact.BranchVulnerabilityUpstreamCVEID} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			branch := supplyChainImpactBranch(t, name)
			if branch.MaxListLength <= 0 {
				t.Fatalf("branch %s has no MaxListLength, want the chunked key probe", branch.Name)
			}
			values := map[factimpact.Param]any{branch.Params[0]: "CVE-2024-00001"}
			routed := &supplyChainImpactRoutingDB{branchRows: map[string][][2]string{}, facts: map[string]string{}}
			var stats supplychaincore.SupplyChainImpactActiveEvidenceLoadStats
			_, err := supplyChainImpactBranchStatements(context.Background(), routed, branch, values, &stats)
			if err == nil {
				t.Fatalf("supplyChainImpactBranchStatements with a non-list param = nil error, want a loud failure")
			}
			if !strings.Contains(err.Error(), branch.Name) {
				t.Fatalf("error = %v, want it to name branch %s", err, branch.Name)
			}
		})
	}
}

// cveChunkBeginCounter counts the snapshots a load opens.
type cveChunkBeginCounter struct {
	*supplyChainImpactRoutingDB
	begins *int
}

func (c cveChunkBeginCounter) BeginReadOnlyRepeatableRead(ctx context.Context) (db.Transaction, error) {
	*c.begins++
	return c.supplyChainImpactRoutingDB.BeginReadOnlyRepeatableRead(ctx)
}

// cveChunkFailAfter fails the key probe's statement number after, counting from 1.
type cveChunkFailAfter struct {
	*supplyChainImpactRoutingDB
	after   int
	failure error
}

func (c cveChunkFailAfter) BeginReadOnlyRepeatableRead(ctx context.Context) (db.Transaction, error) {
	tx, err := c.supplyChainImpactRoutingDB.BeginReadOnlyRepeatableRead(ctx)
	if err != nil {
		return nil, err
	}
	return cveChunkFailingTx{tx, c.supplyChainImpactRoutingDB, c.after, c.failure}, nil
}

type cveChunkFailingTx struct {
	db.Transaction
	routed  *supplyChainImpactRoutingDB
	after   int
	failure error
}

func (tx cveChunkFailingTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	if len(tx.routed.callsFor(cveChunkBranch)) >= tx.after {
		for _, branch := range factimpact.Branches() {
			if branch.Name == cveChunkBranch && branch.SQL == query {
				return nil, tx.failure
			}
		}
	}
	return tx.Transaction.QueryContext(ctx, query, args...)
}
