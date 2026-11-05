// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// walkHistory is an in-memory suppression history: generation id to the CVE id
// each suppression names ("" is a suppression scoped without a CVE). A key
// missing from a generation means that suppression is absent there.
type walkHistory map[string]map[string]string

// diff mirrors the diff statement: a full join on the suppression id, keeping
// the rows whose payload differs.
func (h walkHistory) diff(priorID, currentID string) ([]suppressionDiffRow, error) {
	prior, current := h[priorID], h[currentID]
	var rows []suppressionDiffRow
	for id, cve := range current {
		if old, ok := prior[id]; !ok || old != cve {
			row := suppressionDiffRow{SuppressionID: id, CurrentCVEID: cve}
			if ok {
				row.PriorCVEID = old
			}
			rows = append(rows, row)
		}
	}
	for id, old := range prior {
		if _, ok := current[id]; !ok {
			rows = append(rows, suppressionDiffRow{SuppressionID: id, PriorCVEID: old})
		}
	}
	return rows, nil
}

// bruteForceFanOut is the oracle: for every suppression, every adjacent change in
// its history names both of its CVE ids. It walks per suppression, not per pair.
func (h walkHistory) bruteForceFanOut(chain []string) []string {
	ids := map[string]struct{}{}
	suppressions := map[string]struct{}{}
	for _, generationID := range chain {
		for id := range h[generationID] {
			suppressions[id] = struct{}{}
		}
	}
	for id := range suppressions {
		for i := 0; i+1 < len(chain); i++ {
			before, hadBefore := h[chain[i]][id]
			after, hasAfter := h[chain[i+1]][id]
			if hadBefore == hasAfter && before == after {
				continue
			}
			for _, cve := range []string{before, after} {
				if cve != "" {
					ids[cve] = struct{}{}
				}
			}
		}
	}
	return sortedSetKeys(ids)
}

func walkChain(n int) ([]suppressionGeneration, []string) {
	var candidates []suppressionGeneration
	var chain []string
	for i := 0; i < n; i++ {
		id := string(rune('a' + i))
		at := time.Date(2026, 11, 4, 1+i, 0, 0, 0, time.UTC)
		candidates = append(candidates, suppressionGeneration{
			GenerationID: id, Status: "superseded", IngestedAt: at, ActivatedAt: at, Admitted: i == 0,
		})
		chain = append(chain, id)
	}
	candidates[n-1].Status = "active"
	return candidates, chain
}

func runWalk(t *testing.T, h walkHistory, candidates []suppressionGeneration, current string, maxPairs int) suppressionWalkResult {
	t.Helper()
	baseline := selectSuppressionDiffBaseline(current, candidates)
	plan := planSuppressionWalk(current, baseline, candidates, maxPairs)
	result, err := walkSuppressionDiff(plan, h.diff)
	if err != nil {
		t.Fatalf("walkSuppressionDiff() error = %v", err)
	}
	return result
}

// The endpoint diff of each plant is empty, so a walk that only compares the
// baseline with the current generation returns nothing and hides a finding.
func TestWalkSuppressionDiffNetZeroPlants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		history walkHistory
		want    []string
	}{
		{
			name: "added in b and removed in c, a holder ran during b",
			history: walkHistory{
				"a": {},
				"b": {"x": "CVE-2026-1"},
				"c": {},
			},
			want: []string{"CVE-2026-1"},
		},
		{
			name: "removed in b and re-added unchanged in c",
			history: walkHistory{
				"a": {"x": "CVE-2026-1"},
				"b": {},
				"c": {"x": "CVE-2026-1"},
			},
			want: []string{"CVE-2026-1"},
		},
		{
			name: "re-scope round trip CVE-1 to CVE-2 to CVE-1",
			history: walkHistory{
				"a": {"x": "CVE-2026-1"},
				"b": {"x": "CVE-2026-2"},
				"c": {"x": "CVE-2026-1"},
			},
			want: []string{"CVE-2026-1", "CVE-2026-2"},
		},
		{
			name: "an untouched suppression in every generation names nothing",
			history: walkHistory{
				"a": {"x": "CVE-2026-1"},
				"b": {"x": "CVE-2026-1"},
				"c": {"x": "CVE-2026-1"},
			},
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			candidates, chain := walkChain(3)
			endpoint, _ := tc.history.diff("a", "c")
			if len(endpoint) != 0 {
				t.Fatalf("plant is not net-zero: endpoint diff = %+v", endpoint)
			}
			result := runWalk(t, tc.history, candidates, "c", 8)
			if !reflect.DeepEqual(result.CVEIDs, tc.want) {
				t.Fatalf("walk CVEIDs = %#v, want %#v", result.CVEIDs, tc.want)
			}
			if oracle := tc.history.bruteForceFanOut(chain); !reflect.DeepEqual(result.CVEIDs, oracle) {
				t.Fatalf("walk CVEIDs = %#v, oracle = %#v", result.CVEIDs, oracle)
			}
			if result.WalkTruncated != 0 {
				t.Fatalf("WalkTruncated = %d, want 0 within the cap", result.WalkTruncated)
			}
		})
	}
}

func TestPlanSuppressionWalkSteps(t *testing.T) {
	t.Parallel()

	step := func(prior, current string) suppressionWalkStep {
		return suppressionWalkStep{PriorGenerationID: prior, CurrentGenerationID: current}
	}
	at := func(hour int) time.Time { return time.Date(2026, 11, 4, hour, 0, 0, 0, time.UTC) }

	t.Run("adjacent pairs from the baseline to the current generation", func(t *testing.T) {
		t.Parallel()
		candidates, _ := walkChain(4)
		plan := planSuppressionWalk("d", selectSuppressionDiffBaseline("d", candidates), candidates, 8)
		want := []suppressionWalkStep{step("a", "b"), step("b", "c"), step("c", "d")}
		if !reflect.DeepEqual(plan.Steps, want) || plan.WalkTruncated != 0 {
			t.Fatalf("plan = %+v, want steps %+v and no truncation", plan, want)
		}
	})

	t.Run("a never-activated or failed generation is not in the chain", func(t *testing.T) {
		t.Parallel()
		candidates := []suppressionGeneration{
			{GenerationID: "a", Status: "superseded", IngestedAt: at(1), ActivatedAt: at(1), Admitted: true},
			{GenerationID: "ghost", Status: "superseded", IngestedAt: at(2)},
			{GenerationID: "bad", Status: "failed", IngestedAt: at(3), ActivatedAt: at(3)},
			{GenerationID: "d", Status: "active", IngestedAt: at(4), ActivatedAt: at(4)},
		}
		plan := planSuppressionWalk("d", selectSuppressionDiffBaseline("d", candidates), candidates, 8)
		if want := []suppressionWalkStep{step("a", "d")}; !reflect.DeepEqual(plan.Steps, want) {
			t.Fatalf("steps = %+v, want %+v", plan.Steps, want)
		}
	})

	t.Run("adjacent means ingest order, and a later-ingested generation is out of range", func(t *testing.T) {
		t.Parallel()
		candidates := []suppressionGeneration{
			{GenerationID: "a", Status: "superseded", IngestedAt: at(1), ActivatedAt: at(1), Admitted: true},
			{GenerationID: "b", Status: "superseded", IngestedAt: at(2), ActivatedAt: at(9)},
			{GenerationID: "c", Status: "superseded", IngestedAt: at(3), ActivatedAt: at(4)},
			{GenerationID: "d", Status: "active", IngestedAt: at(4), ActivatedAt: at(5)},
		}
		plan := planSuppressionWalk("c", selectSuppressionDiffBaseline("c", candidates), candidates, 8)
		if want := []suppressionWalkStep{step("a", "b"), step("b", "c")}; !reflect.DeepEqual(plan.Steps, want) {
			t.Fatalf("steps = %+v, want %+v", plan.Steps, want)
		}
	})

	t.Run("the oldest retained fallback walks under the same cap", func(t *testing.T) {
		t.Parallel()
		candidates, _ := walkChain(6)
		candidates[0].Admitted = false
		baseline := selectSuppressionDiffBaseline("f", candidates)
		if baseline.Kind != suppressionBaselineOldestRetained {
			t.Fatalf("baseline kind = %q, want oldest_retained", baseline.Kind)
		}
		plan := planSuppressionWalk("f", baseline, candidates, 2)
		want := []suppressionWalkStep{step("a", "d"), step("d", "e"), step("e", "f")}
		if !reflect.DeepEqual(plan.Steps, want) || plan.WalkTruncated != 2 {
			t.Fatalf("plan = %+v, want steps %+v and 2 unwalked generations", plan, want)
		}
	})

	t.Run("no baseline plans no step", func(t *testing.T) {
		t.Parallel()
		candidates, _ := walkChain(1)
		plan := planSuppressionWalk("a", selectSuppressionDiffBaseline("a", candidates), candidates, 8)
		if len(plan.Steps) != 0 || plan.WalkTruncated != 0 {
			t.Fatalf("plan = %+v, want empty", plan)
		}
	})

	t.Run("a remainder of one pair needs no endpoint and loses nothing", func(t *testing.T) {
		t.Parallel()
		candidates, _ := walkChain(4)
		plan := planSuppressionWalk("d", selectSuppressionDiffBaseline("d", candidates), candidates, 2)
		want := []suppressionWalkStep{step("a", "b"), step("b", "c"), step("c", "d")}
		if !reflect.DeepEqual(plan.Steps, want) || plan.WalkTruncated != 0 {
			t.Fatalf("plan = %+v, want steps %+v and no truncation", plan, want)
		}
	})

	t.Run("a cap below one walks the newest pair", func(t *testing.T) {
		t.Parallel()
		candidates, _ := walkChain(3)
		plan := planSuppressionWalk("c", selectSuppressionDiffBaseline("c", candidates), candidates, 0)
		want := []suppressionWalkStep{step("a", "b"), step("b", "c")}
		if !reflect.DeepEqual(plan.Steps, want) {
			t.Fatalf("steps = %+v, want %+v", plan.Steps, want)
		}
	})
}

func TestWalkSuppressionDiffOverTheCapDegradesLoudly(t *testing.T) {
	t.Parallel()

	// Chain a..f. x is added in b and removed in c (net-zero inside the span the
	// cap leaves unwalked). y is added in e and removed in f (inside the cap).
	history := walkHistory{
		"a": {},
		"b": {"x": "CVE-2026-1"},
		"c": {},
		"d": {},
		"e": {"y": "CVE-2026-2"},
		"f": {},
	}
	candidates, _ := walkChain(6)
	result := runWalk(t, history, candidates, "f", 2)

	if want := []string{"CVE-2026-2"}; !reflect.DeepEqual(result.CVEIDs, want) {
		t.Fatalf("CVEIDs = %#v, want %#v: the walked pairs are exact and the unwalked span is endpoint-only", result.CVEIDs, want)
	}
	if result.WalkTruncated != 2 {
		t.Fatalf("WalkTruncated = %d, want 2 unwalked generations (b and c)", result.WalkTruncated)
	}
	if result.Statements != 3 {
		t.Fatalf("Statements = %d, want 3: the newest two pairs plus one endpoint diff", result.Statements)
	}

	uncapped := runWalk(t, history, candidates, "f", 8)
	if want := []string{"CVE-2026-1", "CVE-2026-2"}; !reflect.DeepEqual(uncapped.CVEIDs, want) || uncapped.WalkTruncated != 0 {
		t.Fatalf("uncapped = %+v, want %#v and no truncation", uncapped, want)
	}
}

func TestWalkSuppressionDiffCountsAndErrors(t *testing.T) {
	t.Parallel()

	t.Run("suppressions without a CVE count once and never a CVE-bearing id twice", func(t *testing.T) {
		t.Parallel()
		history := walkHistory{
			"a": {},
			"b": {"pkg": "", "x": "CVE-2026-1"},
			"c": {"x": "CVE-2026-1", "pkg2": ""},
		}
		candidates, _ := walkChain(3)
		result := runWalk(t, history, candidates, "c", 8)
		// pkg: added in b, removed in c (two rows, one suppression). pkg2: added in c.
		if result.NoCVEScopeRows != 2 {
			t.Fatalf("NoCVEScopeRows = %d, want 2 distinct suppressions", result.NoCVEScopeRows)
		}
		if want := []string{"CVE-2026-1"}; !reflect.DeepEqual(result.CVEIDs, want) {
			t.Fatalf("CVEIDs = %#v, want %#v", result.CVEIDs, want)
		}
		if result.DiffRows != 4 {
			t.Fatalf("DiffRows = %d, want 4 rows read across both pairs", result.DiffRows)
		}
	})

	t.Run("a read error fails the whole walk with no partial set", func(t *testing.T) {
		t.Parallel()
		candidates, _ := walkChain(3)
		plan := planSuppressionWalk("c", selectSuppressionDiffBaseline("c", candidates), candidates, 8)
		boom := errors.New("statement timeout")
		calls := 0
		result, err := walkSuppressionDiff(plan, func(string, string) ([]suppressionDiffRow, error) {
			calls++
			if calls == 2 {
				return nil, boom
			}
			return []suppressionDiffRow{{SuppressionID: "x", CurrentCVEID: "CVE-2026-1"}}, nil
		})
		if !errors.Is(err, boom) {
			t.Fatalf("error = %v, want it to wrap %v", err, boom)
		}
		if len(result.CVEIDs) != 0 {
			t.Fatalf("CVEIDs = %#v, want none on error", result.CVEIDs)
		}
	})

	t.Run("an empty plan reads nothing", func(t *testing.T) {
		t.Parallel()
		result, err := walkSuppressionDiff(suppressionWalkPlan{}, func(string, string) ([]suppressionDiffRow, error) {
			t.Fatal("read called for an empty plan")
			return nil, nil
		})
		if err != nil || len(result.CVEIDs) != 0 || result.Statements != 0 {
			t.Fatalf("result = %+v, err = %v, want empty", result, err)
		}
	})
}

// A long chain must never error and must never undercount a pair inside the cap.
func TestWalkSuppressionDiffMatchesOracleOnALongChain(t *testing.T) {
	t.Parallel()

	candidates, chain := walkChain(12)
	history := walkHistory{}
	for i, id := range chain {
		history[id] = map[string]string{"keep": "CVE-2026-0"}
		if i == 3 || i == 4 {
			history[id]["x"] = "CVE-2026-1"
		}
		if i == 9 {
			history[id]["y"] = "CVE-2026-2"
		}
	}
	result := runWalk(t, history, candidates, "l", len(chain))
	if oracle := history.bruteForceFanOut(chain); !slices.Equal(result.CVEIDs, oracle) {
		t.Fatalf("walk = %#v, oracle = %#v", result.CVEIDs, oracle)
	}
}

func TestSuppressionDiffFailsClosedWithTheExistingDeferral(t *testing.T) {
	t.Parallel()

	err := suppressionDiffDeferUnactivated.failClosedError("scope-1", "gen-1")
	if !errors.Is(err, reducercontract.ErrGenerationNotYetActive) {
		t.Fatalf("error = %v, want ErrGenerationNotYetActive", err)
	}
	var notActive reducercontract.GenerationNotYetActiveError
	if !errors.As(err, &notActive) || notActive.ScopeID != "scope-1" || notActive.GenerationID != "gen-1" {
		t.Fatalf("error = %#v, want GenerationNotYetActiveError for scope-1 gen-1", err)
	}
	if !notActive.Retryable() || notActive.FailureClass() != reducercontract.GenerationActivationNotReadyFailureClass {
		t.Fatalf("deferral must be retryable and non-counting, got class %q", notActive.FailureClass())
	}
	for _, action := range []suppressionDiffAction{
		suppressionDiffRun, suppressionDiffSkipKind, suppressionDiffSkipDuplicate, suppressionDiffSkipNeverActivated,
	} {
		if err := action.failClosedError("scope-1", "gen-1"); err != nil {
			t.Fatalf("%s must not fail closed, got %v", action, err)
		}
	}
}

// The default cap bounds the statements one pass issues inside the writer
// transaction, however long the retained chain is.
func TestSuppressionWalkMaxPairsBoundsStatements(t *testing.T) {
	t.Parallel()

	candidates, _ := walkChain(26)
	plan := planSuppressionWalk("z", selectSuppressionDiffBaseline("z", candidates), candidates, suppressionWalkMaxPairs)
	if len(plan.Steps) != suppressionWalkMaxPairs+1 {
		t.Fatalf("statements = %d, want %d: the capped pairs plus one endpoint diff", len(plan.Steps), suppressionWalkMaxPairs+1)
	}
	if plan.WalkTruncated != 25-suppressionWalkMaxPairs-1 {
		t.Fatalf("WalkTruncated = %d, want %d", plan.WalkTruncated, 25-suppressionWalkMaxPairs-1)
	}
}
