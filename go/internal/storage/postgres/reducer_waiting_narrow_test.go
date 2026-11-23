// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// Narrowed waiting key unit proofs (#10877): the verdict-time sets render
// deterministically, extraction unwraps the Fail-path wrapping, and the
// wrong kinds or empty sets fall back to the coarse wait.

type narrowUnitTestCause struct {
	repos    []string
	blocking []string
}

func (e narrowUnitTestCause) Error() string { return "narrow unit test cause" }

func (e narrowUnitTestCause) CorpusWaitRepoIDs() []string { return e.repos }

func (e narrowUnitTestCause) CorpusWaitBlockingScopeIDs() []string { return e.blocking }

func narrowUnitIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-narrow-unit",
		ScopeID:      "scope-unit",
		GenerationID: "gen-unit",
		Domain:       "workload_materialization",
	}
}

func narrowUnitEntry() WaitingDeferral {
	return WaitingDeferral{
		FailureClass:  "workload_materialization_resolution_not_ready",
		WaitsForKind:  WaitingKindRelationshipGenerationActive,
		WakeSite:      WaitingWakeSiteInTx,
		ProvidingSave: "test",
	}
}

func TestNarrowedWaitingKeyRendersDeterministically(t *testing.T) {
	t.Parallel()
	firstKey, ok := narrowedWaitingKey("scope-unit", "gen-unit", "wi-unit",
		[]string{"scope-b", "scope-a", "scope-a", " "}, []string{"repo-b", "repo-a"})
	first := firstKey.Key
	if !ok {
		t.Fatal("narrowedWaitingKey() = false, want a key")
	}
	secondKey, ok := narrowedWaitingKey("scope-unit", "gen-unit", "wi-unit",
		[]string{"scope-a", "scope-b"}, []string{"repo-a", "repo-b"})
	second := secondKey.Key
	if !ok {
		t.Fatal("narrowedWaitingKey() = false, want a key")
	}
	if first != second {
		t.Fatalf("narrowed keys differ:\n%s\n%s", first, second)
	}
	if _, ok := narrowedWaitingKey("scope-unit", "gen-unit", "wi-unit", nil, []string{"repo-a"}); ok {
		t.Fatal("narrowedWaitingKey() with empty blocking = true, want false (fall back to coarse)")
	}
	if _, ok := narrowedWaitingKey("", "gen-unit", "wi-unit", []string{"scope-a"}, nil); ok {
		t.Fatal("narrowedWaitingKey() with empty scope = true, want false")
	}
}

func TestNarrowedWaitingKeyForCauseUnwraps(t *testing.T) {
	t.Parallel()
	cause := narrowUnitTestCause{repos: []string{"repo-a"}, blocking: []string{"scope-b"}}
	wrapped := fmt.Errorf("load resolved relationships: %w", cause)
	narrowedKey, ok := narrowedWaitingKeyForCause(narrowUnitIntent(), narrowUnitEntry(), wrapped)
	keyJSON := narrowedKey.Key
	if !ok {
		t.Fatal("narrowedWaitingKeyForCause() = false for a wrapped narrowing cause, want a key")
	}
	var key waitingKey
	if err := json.Unmarshal([]byte(keyJSON), &key); err != nil {
		t.Fatalf("decode narrowed key: %v", err)
	}
	if key.ScopeID != "scope-unit" || key.GenerationID != "gen-unit" {
		t.Fatalf("narrowed key scope/gen = %s/%s, want scope-unit/gen-unit", key.ScopeID, key.GenerationID)
	}
	if len(key.BlockingScopeIDs) != 1 || key.BlockingScopeIDs[0] != "scope-b" {
		t.Fatalf("narrowed key blocking = %v, want [scope-b]", key.BlockingScopeIDs)
	}
	if len(key.RepoIDs) != 1 || key.RepoIDs[0] != "repo-a" {
		t.Fatalf("narrowed key repos = %v, want [repo-a]", key.RepoIDs)
	}
}

func TestNarrowedWaitingKeyForCauseRejects(t *testing.T) {
	t.Parallel()
	cause := narrowUnitTestCause{repos: []string{"repo-a"}, blocking: []string{"scope-b"}}
	if _, ok := narrowedWaitingKeyForCause(narrowUnitIntent(), narrowUnitEntry(), nil); ok {
		t.Fatal("nil cause parks narrowed, want the coarse wait")
	}
	if _, ok := narrowedWaitingKeyForCause(narrowUnitIntent(), narrowUnitEntry(), fmt.Errorf("plain")); ok {
		t.Fatal("non-narrowing cause parks narrowed, want the coarse wait")
	}
	other := narrowUnitEntry()
	other.WaitsForKind = WaitingKindFenceClear
	if _, ok := narrowedWaitingKeyForCause(narrowUnitIntent(), other, cause); ok {
		t.Fatal("non-corpus kind parks narrowed, want the coarse wait")
	}
}

// TestSetAsideWaitingForCauseNeverParksCountingClasses pins the union
// guard (#10877 decision 5, seam iv): counting and not-a-wait entries park
// no row even when the cause carries a narrowing verdict-time set. The nil
// database proves the guard short-circuits before any storage touch: a
// regression that drops the ForCause guard panics on the nil database
// instead of returning (false, nil).
func TestSetAsideWaitingForCauseNeverParksCountingClasses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	now := time.Now().UTC()
	causes := map[string]error{
		"narrowing":    narrowUnitTestCause{repos: []string{"repo-a"}, blocking: []string{"scope-a"}},
		"nonnarrowing": fmt.Errorf("plain retryable"),
	}
	entries := map[string]WaitingDeferral{
		"counting": {
			FailureClass: "test_counting_class",
			WaitsForKind: WaitingKindRelationshipGenerationActive,
			WakeSite:     WaitingWakeSiteCounting,
		},
		"not_a_wait": {
			FailureClass: "test_write_superseded_class",
			WaitsForKind: WaitingKindRelationshipGenerationActive,
			WakeSite:     WaitingWakeSiteNotAWait,
		},
	}
	for causeName, cause := range causes {
		for entryName, entry := range entries {
			parked, err := setAsideWaitingForCause(ctx, nil, nil,
				narrowUnitIntent(), entry, cause, nil, now)
			if err != nil || parked {
				t.Fatalf("setAsideWaitingForCause(%s/%s) = (%v, %v), want (false, nil)",
					causeName, entryName, parked, err)
			}
		}
	}
}

// captureWaitingQueryDB is a hermetic db.ExecQueryer: it records the query
// and bind args of the single-row read and answers a canned verdict.
type captureWaitingQueryDB struct {
	query string
	args  []any
	value bool
}

func (f *captureWaitingQueryDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	f.query = query
	f.args = args
	return &singleBoolWaitingRows{value: f.value}, nil
}

func (f *captureWaitingQueryDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

// singleBoolWaitingRows answers one boolean column, then ends.
type singleBoolWaitingRows struct {
	value bool
	done  bool
}

func (r *singleBoolWaitingRows) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}

func (r *singleBoolWaitingRows) Scan(dest ...any) error {
	got, ok := dest[0].(*bool)
	if !ok {
		return fmt.Errorf("singleBoolWaitingRows.Scan: dest %T, want *bool", dest[0])
	}
	*got = r.value
	return nil
}

func (r *singleBoolWaitingRows) Err() error { return nil }

func (r *singleBoolWaitingRows) Close() error { return nil }

// boundTextSet extracts one bind arg as a text set and proves it carries the
// exact elements: the arg must be array.StringArray, and its driver literal
// must scan back to the identical set.
func boundTextSet(t *testing.T, args []any, index int, want []string) {
	t.Helper()
	if index >= len(args) {
		t.Fatalf("bind args have length %d, want an arg at index %d", len(args), index)
	}
	got, ok := args[index].(array.StringArray)
	if !ok {
		t.Fatalf("bind arg %d has type %T, want array.StringArray (hand-built text literal)", index, args[index])
	}
	if len(got) != len(want) {
		t.Fatalf("bind arg %d carries %v, want %v", index, []string(got), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bind arg %d carries %v, want %v", index, []string(got), want)
		}
	}
	literal, err := got.Value()
	if err != nil {
		t.Fatalf("array.StringArray.Value(): %v", err)
	}
	var roundTrip array.StringArray
	if err := roundTrip.Scan(literal); err != nil {
		t.Fatalf("array.StringArray.Scan(%v): %v", literal, err)
	}
	if len(roundTrip) != len(want) {
		t.Fatalf("bind arg %d round-trips to %v, want %v", index, []string(roundTrip), want)
	}
	for i := range want {
		if roundTrip[i] != want[i] {
			t.Fatalf("bind arg %d round-trips to %v, want %v", index, []string(roundTrip), want)
		}
	}
}

// TestWaitingSatisfiedBindsTextSetsAsArrays pins the #10877 follow-up P2:
// the narrowed ($3) and coarse ($6, $9) satisfaction predicates must bind
// their text[] sets with the driver's array type, not a hand-concatenated
// literal. The old quoteSQLStrings escaped only double quotes, so a scope id
// carrying a backslash parsed to a different value in the shared wait/audit
// predicate (a missed wake bounded by the audit and the backstop, or a
// spurious wake costing one re-deferral).
func TestWaitingSatisfiedBindsTextSetsAsArrays(t *testing.T) {
	t.Parallel()
	odd := []string{`scope\back`, `scope"quote`}
	now := time.Now().UTC()

	t.Run("narrowed blocking set", func(t *testing.T) {
		t.Parallel()
		fake := &captureWaitingQueryDB{}
		if _, err := waitingSatisfied(context.Background(), fake,
			WaitingKindRelationshipGenerationActive,
			"scope-w", "gen-w", "", "", "waiter-1", nil, odd, waitingPhaseRequirement{}, now); err != nil {
			t.Fatalf("waitingSatisfied(): %v", err)
		}
		boundTextSet(t, fake.args, 2, odd)
	})

	t.Run("fence domains", func(t *testing.T) {
		t.Parallel()
		fake := &captureWaitingQueryDB{}
		if _, err := waitingSatisfied(context.Background(), fake,
			WaitingKindFenceClear,
			"scope-w", "gen-w", "", "", "waiter-1", odd, nil, waitingPhaseRequirement{}, now); err != nil {
			t.Fatalf("waitingSatisfied(): %v", err)
		}
		boundTextSet(t, fake.args, 5, odd)
	})

	t.Run("producer collector kinds", func(t *testing.T) {
		t.Parallel()
		fake := &captureWaitingQueryDB{}
		if _, err := waitingSatisfied(context.Background(), fake,
			WaitingKindProducerTerminal,
			"", "", string(reducercontract.DomainCICDRunCorrelation), "", "waiter-1", nil, nil, waitingPhaseRequirement{}, now); err != nil {
			t.Fatalf("waitingSatisfied(): %v", err)
		}
		boundTextSet(t, fake.args, 8, []string{"ci_cd_run"})
	})
}
