// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// incompressibleIDs returns n distinct sorted IDs of byteLen random printable
// bytes each, the worst case for btree key compression.
func incompressibleIDs(t testing.TB, n, byteLen int) []string {
	t.Helper()
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	seen := make(map[string]struct{}, n)
	out := make([]string, 0, n)
	buf := make([]byte, byteLen)
	for len(out) < n {
		if _, err := cryptorand.Read(buf); err != nil {
			t.Fatalf("random id: %v", err)
		}
		for i := range buf {
			buf[i] = alphabet[int(buf[i])%len(alphabet)]
		}
		id := string(buf)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// TestNarrowedWaitingKeyIsBoundedByConstruction pins the #11256 key contract:
// the key is {scope_id, generation_id, blocking_scope_ids}, the blocking set
// is the longest sorted prefix of the verdict set within both caps, and the
// key never carries the read set. Whatever the verdict-set size, the key fits
// the two btree indexes it lives in.
func TestNarrowedWaitingKeyIsBoundedByConstruction(t *testing.T) {
	t.Parallel()
	sets := map[string]func(n int) []string{
		"real-shape git scope IDs": func(n int) []string { return realShapeScopeIDs(t, n) },
		"incompressible 200-byte":  func(n int) []string { return incompressibleIDs(t, n, 200) },
	}
	for name, gen := range sets {
		for _, n := range []int{1, 5, 16, 17, 200, 5000} {
			t.Run(fmt.Sprintf("%s/%d", name, n), func(t *testing.T) {
				t.Parallel()
				ids := gen(n)
				key, ok := narrowedWaitingKey("scope-waiter", "gen-waiter", ids)
				if !ok || key.JSON == "" {
					t.Fatalf("narrowedWaitingKey = ok %v, key %q, want a key", ok, key.JSON)
				}
				if got := len(key.JSON); got > narrowedWaitKeyMaxBytes {
					t.Fatalf("rendered key is %d bytes, want at most %d", got, narrowedWaitKeyMaxBytes)
				}
				var decoded waitingKey
				if err := json.Unmarshal([]byte(key.JSON), &decoded); err != nil {
					t.Fatalf("decode key: %v", err)
				}
				if len(decoded.BlockingScopeIDs) == 0 || len(decoded.BlockingScopeIDs) > narrowedWaitKeyMaxBlockingScopes {
					t.Fatalf("named blockers = %d, want 1 to %d", len(decoded.BlockingScopeIDs), narrowedWaitKeyMaxBlockingScopes)
				}
				if !slices.Equal(decoded.BlockingScopeIDs, ids[:len(decoded.BlockingScopeIDs)]) {
					t.Fatalf("named blockers are not the sorted prefix of the verdict set")
				}
				if key.Named != len(decoded.BlockingScopeIDs) || key.Total != len(ids) {
					t.Fatalf("named/total = %d/%d, want %d/%d", key.Named, key.Total, len(decoded.BlockingScopeIDs), len(ids))
				}
				if decoded.ScopeID != "scope-waiter" || decoded.GenerationID != "gen-waiter" {
					t.Fatalf("key scope/generation = %q/%q", decoded.ScopeID, decoded.GenerationID)
				}
				if strings.Contains(key.JSON, "repo_ids") {
					t.Fatalf("key carries repo_ids: %s", key.JSON)
				}
				// Order independence: the same set in any order renders the
				// same key, so a repeated park stays one idempotent row.
				shuffled := slices.Clone(ids)
				rand.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
				again, _ := narrowedWaitingKey("scope-waiter", "gen-waiter", shuffled)
				if again.JSON != key.JSON {
					t.Fatalf("key depends on input order:\n%s\n%s", key.JSON, again.JSON)
				}
				if n <= narrowedWaitKeyMaxBlockingScopes && len(ids[len(ids)-1]) < 80 && key.Named != n {
					t.Fatalf("a set of %d real-shape IDs names %d, want all of them", n, key.Named)
				}
			})
		}
	}
}

// TestNarrowedWaitingKeySkipsAnIDThatCannotFitAlone pins the skip rule: an ID
// larger than the byte budget on its own is left out and the rest are named;
// a set where nothing fits yields no key (the caller treats it as oversize,
// never as the coarse wait, which would be satisfied at park and spin).
func TestNarrowedWaitingKeySkipsAnIDThatCannotFitAlone(t *testing.T) {
	t.Parallel()
	huge := strings.Repeat("x", 2*narrowedWaitKeyMaxBytes)
	key, ok := narrowedWaitingKey("scope-waiter", "gen-waiter", []string{"scope-a", huge, "scope-b"})
	if !ok {
		t.Fatal("ok = false, want a key naming the IDs that fit")
	}
	var decoded waitingKey
	if err := json.Unmarshal([]byte(key.JSON), &decoded); err != nil {
		t.Fatalf("decode key: %v", err)
	}
	if want := []string{"scope-a", "scope-b"}; !slices.Equal(decoded.BlockingScopeIDs, want) {
		t.Fatalf("named blockers = %v, want %v", decoded.BlockingScopeIDs, want)
	}
	if key.Named != 2 || key.Total != 3 {
		t.Fatalf("named/total = %d/%d, want 2/3", key.Named, key.Total)
	}
	none, ok := narrowedWaitingKey("scope-waiter", "gen-waiter", []string{huge})
	if !ok || none.JSON != "" || none.Named != 0 || none.Total != 1 {
		t.Fatalf("all-oversize set = ok %v key %q named %d total %d, want ok, no key, 0 named, total 1", ok, none.JSON, none.Named, none.Total)
	}
	if _, ok := narrowedWaitingKey("scope-waiter", "gen-waiter", nil); ok {
		t.Fatal("empty blocking set ok = true, want false (the coarse wait)")
	}
}

// keyBoundDB scripts the statements a park issues, recording the wait-row
// inserts.
type keyBoundDB struct {
	inserts []string
}

func (d *keyBoundDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	if query == insertWaitingItemQuery {
		d.inserts = append(d.inserts, args[0].(string))
	}
	return fakeResult{}, nil
}

type keyBoundBoolRows struct{ done bool }

func (r *keyBoundBoolRows) Next() bool { d := !r.done; r.done = true; return d }
func (r *keyBoundBoolRows) Scan(dest ...any) error {
	*(dest[0].(*bool)) = false
	return nil
}
func (r *keyBoundBoolRows) Err() error   { return nil }
func (r *keyBoundBoolRows) Close() error { return nil }

func (d *keyBoundDB) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	if query == tryBlockingActivationsQuery {
		return &rendezvousRows{}, nil
	}
	return &keyBoundBoolRows{}, nil
}

func keyBoundMetrics(t *testing.T) (*telemetry.Instruments, func(name string) int64) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("keybound"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	return instruments, func(name string) int64 {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("collect: %v", err)
		}
		var total int64
		for _, scope := range rm.ScopeMetrics {
			for _, m := range scope.Metrics {
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok && m.Name == name {
					for _, point := range sum.DataPoints {
						total += point.Value
					}
				}
			}
		}
		return total
	}
}

func keyBoundIntent(workItemID string) reducercontract.Intent {
	return reducercontract.Intent{
		IntentID: workItemID, ScopeID: "git-repository-scope:repository:r_aaaaaaaa",
		GenerationID: "gen-1", Domain: "workload_materialization",
	}
}

// TestInsertWaitingItemRefusesAnOversizeRow pins the size guard: a wait row
// whose work_item_id, kind and key together exceed what both btree indexes
// take is not inserted (the insert would fail with SQLSTATE 54000 and stop
// the reducer); the oversize counter records it, the park reports no wait
// row and no error, and the queue row keeps the timer path.
func TestInsertWaitingItemRefusesAnOversizeRow(t *testing.T) {
	t.Parallel()
	instruments, count := keyBoundMetrics(t)
	fake := &keyBoundDB{}
	entry := WaitingDeferral{WaitsForKind: WaitingKindGenerationActive, WakeSite: WaitingWakeSiteInTx}
	oversize := fmt.Sprintf(`{"scope_id":%q,"generation_id":"gen-1"}`, strings.Repeat("s", waitingRowIndexBudgetBytes))
	inserted, err := insertWaitingItem(context.Background(), fake, instruments, keyBoundIntent("item-1"), entry, oversize, time.Now())
	if err != nil || inserted {
		t.Fatalf("insertWaitingItem(oversize) = inserted %v, err %v, want false, nil", inserted, err)
	}
	if len(fake.inserts) != 0 {
		t.Fatalf("oversize row was inserted: %v", fake.inserts)
	}
	if got := count("eshu_dp_reducer_waiting_key_oversize_total"); got != 1 {
		t.Fatalf("oversize counter = %d, want 1", got)
	}
	fits := fmt.Sprintf(`{"scope_id":%q,"generation_id":"gen-1"}`, strings.Repeat("s", 600))
	inserted, err = insertWaitingItem(context.Background(), fake, instruments, keyBoundIntent("item-1"), entry, fits, time.Now())
	if err != nil || !inserted || len(fake.inserts) != 1 {
		t.Fatalf("insertWaitingItem(fits) = inserted %v, err %v, inserts %d, want true, nil, 1", inserted, err, len(fake.inserts))
	}
	if got := count("eshu_dp_reducer_waiting_key_oversize_total"); got != 1 {
		t.Fatalf("oversize counter = %d after an accepted row, want still 1", got)
	}
}

// TestSetAsideWideNarrowedCauseNeverFallsBackToTheCoarseWait pins the
// no-fallback rule: a narrowing deferral whose blocking set has no ID that
// fits the key parks nothing (the coarse wait is satisfied by the waiter's
// own generation being active and would reopen the item at once, in a loop),
// records the oversize counter and returns no error.
func TestSetAsideWideNarrowedCauseNeverFallsBackToTheCoarseWait(t *testing.T) {
	t.Parallel()
	instruments, count := keyBoundMetrics(t)
	fake := &keyBoundDB{}
	cause := wideWaitCause{blocking: []string{strings.Repeat("x", 2*narrowedWaitKeyMaxBytes)}}
	parked, err := setAsideWaitingForCause(context.Background(), fake, instruments,
		keyBoundIntent("item-2"), narrowWaitEntry(), cause, nil, time.Now())
	if err != nil || parked {
		t.Fatalf("setAsideWaitingForCause = parked %v, err %v, want false, nil", parked, err)
	}
	if len(fake.inserts) != 0 {
		t.Fatalf("a wait row was parked: %v", fake.inserts)
	}
	if got := count("eshu_dp_reducer_waiting_key_oversize_total"); got != 1 {
		t.Fatalf("oversize counter = %d, want 1", got)
	}
}

// TestSetAsideCountsATruncatedBlockingSet pins the truncation signal: a
// verdict set larger than the named prefix still parks one row, names the
// prefix only, and counts the truncation once.
func TestSetAsideCountsATruncatedBlockingSet(t *testing.T) {
	t.Parallel()
	instruments, count := keyBoundMetrics(t)
	fake := &keyBoundDB{}
	ids := realShapeScopeIDs(t, 100)
	cause := wideWaitCause{blocking: ids, repos: ids}
	if _, err := setAsideWaitingForCause(context.Background(), fake, instruments,
		keyBoundIntent("item-3"), narrowWaitEntry(), cause, nil, time.Now()); err != nil {
		t.Fatalf("setAsideWaitingForCause: %v", err)
	}
	if len(fake.inserts) != 1 {
		t.Fatalf("wait rows parked = %d, want 1", len(fake.inserts))
	}
	if got := count("eshu_dp_reducer_waiting_key_truncated_total"); got != 1 {
		t.Fatalf("truncated counter = %d, want 1", got)
	}
	if got := count("eshu_dp_reducer_waiting_key_oversize_total"); got != 0 {
		t.Fatalf("oversize counter = %d, want 0 for a key that fits", got)
	}
	small := wideWaitCause{blocking: ids[:3]}
	if _, err := setAsideWaitingForCause(context.Background(), fake, instruments,
		keyBoundIntent("item-4"), narrowWaitEntry(), small, nil, time.Now()); err != nil {
		t.Fatalf("setAsideWaitingForCause(small): %v", err)
	}
	if got := count("eshu_dp_reducer_waiting_key_truncated_total"); got != 1 {
		t.Fatalf("truncated counter = %d after an untruncated park, want still 1", got)
	}
}
