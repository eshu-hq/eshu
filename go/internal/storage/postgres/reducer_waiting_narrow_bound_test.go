// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// #11297: reducer_waiting_items_pkey is a btree over
// (work_item_id, waits_for_kind, waits_for_key). Postgres refuses an index row
// above ~2704 bytes (SQLSTATE 54000 "index row size N exceeds btree version 4
// maximum 2704"). A narrowed corpus wait keyed on the full verdict-time
// blocking set blew past that on a large fleet, the park INSERT failed inside
// WorkSink.Fail, and the batch worker treated the broken Fail as fatal: the
// reducer exited and crash-looped. The narrowed key must stay bounded however
// many scopes hold the fence.

// narrowedKeyIndexBudgetBytes is the most JSON bytes a parked wait key may
// carry. It leaves the work_item_id (~140 bytes, longer for long partition
// keys), the kind, and the JSONB per-element overhead well inside the 2704
// byte btree row limit.
const narrowedKeyIndexBudgetBytes = 2000

// boundWaitingDB records the park INSERT key and answers the satisfaction
// probe as "not satisfied", so the wait stays parked.
type boundWaitingDB struct {
	insertedKeys []string
}

func (f *boundWaitingDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return &singleBoolWaitingRows{value: false}, nil
}

func (f *boundWaitingDB) ExecContext(_ context.Context, _ string, args ...any) (sql.Result, error) {
	if len(args) >= 3 {
		if key, ok := args[2].(string); ok {
			f.insertedKeys = append(f.insertedKeys, key)
		}
	}
	return nil, nil
}

func manyScopeIDs(n int) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("git-repository-scope:repository:r_%08x", i*2654435761))
	}
	return out
}

func TestNarrowedWaitingKeyStaysWithinIndexBudget(t *testing.T) {
	t.Parallel()
	blocking := manyScopeIDs(600)
	repos := manyScopeIDs(400)

	keyJSON, ok := narrowedWaitingKey("scope-unit", "gen-unit", blocking, repos)
	if !ok {
		t.Fatal("narrowedWaitingKey() = false, want a key for a large blocking set")
	}
	if len(keyJSON) > narrowedKeyIndexBudgetBytes {
		t.Fatalf("narrowed key is %d bytes for %d blocking scopes, want <= %d (btree row limit)",
			len(keyJSON), len(blocking), narrowedKeyIndexBudgetBytes)
	}
	var key waitingKey
	if err := json.Unmarshal([]byte(keyJSON), &key); err != nil {
		t.Fatalf("decode narrowed key: %v", err)
	}
	if len(key.BlockingScopeIDs) == 0 {
		t.Fatal("bounded key dropped the whole blocking set; the activation wake would never match")
	}
	want := normalizeWaitingScopeIDs(blocking)
	if !slices.Equal(key.BlockingScopeIDs, want[:len(key.BlockingScopeIDs)]) {
		t.Fatal("bounded blocking set is not the sorted prefix of the verdict-time set")
	}
	again, _ := narrowedWaitingKey("scope-unit", "gen-unit", blocking, repos)
	if again != keyJSON {
		t.Fatal("bounded key is not deterministic for equal inputs")
	}
}

func TestSetAsideWaitingForCauseParksBoundedKeyForLargeBlockingSet(t *testing.T) {
	t.Parallel()
	fake := &boundWaitingDB{}
	cause := narrowUnitTestCause{repos: manyScopeIDs(400), blocking: manyScopeIDs(600)}
	parked, err := setAsideWaitingForCause(context.Background(), fake, nil,
		narrowUnitIntent(), narrowUnitEntry(), cause, nil, time.Now().UTC())
	if err != nil || !parked {
		t.Fatalf("setAsideWaitingForCause() = (%v, %v), want (true, nil)", parked, err)
	}
	if len(fake.insertedKeys) != 1 {
		t.Fatalf("park inserted %d keys, want 1", len(fake.insertedKeys))
	}
	if got := len(fake.insertedKeys[0]); got > narrowedKeyIndexBudgetBytes {
		t.Fatalf("parked waits_for_key is %d bytes, want <= %d: Postgres rejects it as an oversized btree row", got, narrowedKeyIndexBudgetBytes)
	}
}
