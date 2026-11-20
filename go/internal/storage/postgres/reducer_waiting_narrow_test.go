// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
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
	first, ok := narrowedWaitingKey("scope-unit", "gen-unit",
		[]string{"scope-b", "scope-a", "scope-a", " "}, []string{"repo-b", "repo-a"})
	if !ok {
		t.Fatal("narrowedWaitingKey() = false, want a key")
	}
	second, ok := narrowedWaitingKey("scope-unit", "gen-unit",
		[]string{"scope-a", "scope-b"}, []string{"repo-a", "repo-b"})
	if !ok {
		t.Fatal("narrowedWaitingKey() = false, want a key")
	}
	if first != second {
		t.Fatalf("narrowed keys differ:\n%s\n%s", first, second)
	}
	if _, ok := narrowedWaitingKey("scope-unit", "gen-unit", nil, []string{"repo-a"}); ok {
		t.Fatal("narrowedWaitingKey() with empty blocking = true, want false (fall back to coarse)")
	}
	if _, ok := narrowedWaitingKey("", "gen-unit", []string{"scope-a"}, nil); ok {
		t.Fatal("narrowedWaitingKey() with empty scope = true, want false")
	}
}

func TestNarrowedWaitingKeyForCauseUnwraps(t *testing.T) {
	t.Parallel()
	cause := narrowUnitTestCause{repos: []string{"repo-a"}, blocking: []string{"scope-b"}}
	wrapped := fmt.Errorf("load resolved relationships: %w", cause)
	keyJSON, ok := narrowedWaitingKeyForCause(narrowUnitIntent(), narrowUnitEntry(), wrapped)
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
