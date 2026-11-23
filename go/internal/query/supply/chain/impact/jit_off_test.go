// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// routeFake is the route tests' reader: the recording queryer plus a snapshot
// beginner whose transaction records the order of what ran inside it, so a test
// can prove which statements ran under a transaction-local jit = off and that
// nothing touches the transaction after it ends.
type routeFake struct {
	*cov7StoreQueryer
	mu     sync.Mutex
	events []string
}

func (f *routeFake) record(event string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}

func (f *routeFake) BeginReadOnlySnapshot(context.Context) (db.ReadTransaction, error) {
	f.record("begin")
	return &routeFakeTx{fake: f}, nil
}

type routeFakeTx struct {
	fake  *routeFake
	ended bool
}

func (tx *routeFakeTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	tx.fake.record(tx.event(query))
	return tx.fake.QueryContext(ctx, query, args...)
}

func (tx *routeFakeTx) QueryRowContext(ctx context.Context, query string, args ...any) db.Row {
	tx.fake.record(tx.event(query))
	return tx.fake.QueryRowContext(ctx, query, args...)
}

// event names a statement run inside the transaction: the jit switch, or the
// read of one route.
func (tx *routeFakeTx) event(query string) string {
	if tx.ended {
		return "USED AFTER END"
	}
	if strings.Contains(query, "set_config('jit'") {
		return "jit_off"
	}
	if route := routeOf(query); route != "" {
		return "read:" + string(route)
	}
	return "other"
}

func (tx *routeFakeTx) Commit() error   { tx.fake.record("commit"); tx.ended = true; return nil }
func (tx *routeFakeTx) Rollback() error { tx.fake.record("rollback"); tx.ended = true; return nil }

// TestJITIsOffOnlyForTheSupplementAndImageReads pins the ruling's JIT rule: the
// measured pair showed jit = off at least 20 percent faster for the supplement
// and image reads, so those two routes (and no other) run their one statement
// inside a short read transaction after a transaction-local jit = off. The setting
// is transaction-local (set_config(..., true)), never session or pool wide, and the
// transaction ends before the call returns.
func TestJITIsOffOnlyForTheSupplementAndImageReads(t *testing.T) {
	t.Parallel()
	for _, tc := range existenceFilters {
		wantJIT := tc.want == RouteWinnersSupplement || tc.want == RouteSupportsImage
		for what, call := range map[string]func(*routeFake) error{
			"list": func(f *routeFake) error {
				filter := FindingFilter{Limit: 10, Status: "affected_exact", ImageRef: tc.image, DetectionProfile: tc.profile}
				if tc.granted {
					filter.AllowedScopeIDs = []string{"scope:a"}
				}
				_, err := NewPostgresFindingStoreWithReadStore(f, true).ListSupplyChainImpactFindings(t.Context(), filter)
				return err
			},
			"count": func(f *routeFake) error {
				filter := AggregateFilter{Status: "affected_exact", ImageRef: tc.image, DetectionProfile: tc.profile}
				if tc.granted {
					filter.AllowedRepositoryIDs = []string{"repository:r"}
				}
				_, err := NewPostgresAggregateStoreWithReadStore(f, true).CountSupplyChainImpactFindings(t.Context(), filter)
				return err
			},
			"inventory": func(f *routeFake) error {
				filter := AggregateFilter{Status: "affected_exact", ImageRef: tc.image, DetectionProfile: tc.profile}
				if tc.granted {
					filter.AllowedRepositoryIDs = []string{"repository:r"}
				}
				_, err := NewPostgresAggregateStoreWithReadStore(f, true).SupplyChainImpactInventory(t.Context(), filter, InventoryByStatus, 10, 0)
				return err
			},
			"grouped": func(f *routeFake) error {
				store := NewPostgresGroupedInventoryStoreWithReadStore(f, true, nil)
				store.Now = func() time.Time { return time.Now().UTC() }
				filter := AggregateFilter{ImageRef: tc.image, DetectionProfile: tc.profile}
				if tc.granted {
					filter.AllowedScopeIDs = []string{"scope:a"}
				}
				_, err := store.SupplyChainImpactGroupedInventory(t.Context(), filter, GroupedInventoryByService, 10, 0)
				return err
			},
		} {
			t.Run(tc.name+"/"+what, func(t *testing.T) {
				t.Parallel()
				fake := routeQueryer()
				if err := call(fake); err != nil {
					t.Fatalf("%s = %v", what, err)
				}
				got := strings.Join(fake.events, ",")
				if !wantJIT {
					if got != "" {
						t.Fatalf("route %q opened a transaction: %s", tc.want, got)
					}
					return
				}
				want := "begin,jit_off,read:" + string(tc.want) + ",commit"
				if got != want {
					t.Fatalf("route %q ran %q, want %q", tc.want, got, want)
				}
			})
		}
	}
}

// TestExplainNeverSwitchesJITOff pins that the transaction-local jit switch is
// confined to the list, count, inventory and grouped reads of the supplement and
// image routes: no explain lookup (granted, public id, image selector, legacy
// selector) sets it, even though an explain opens its own snapshot transaction.
func TestExplainNeverSwitchesJITOff(t *testing.T) {
	t.Parallel()
	granted := []string{"scope:a"}
	for name, filter := range map[string]packet.ExplanationFilter{
		"granted selector":       {CVEID: "CVE-2026-1", PackageID: "pkg:npm/p", AllowedScopeIDs: granted},
		"ungranted public id":    {FindingID: "finding:a"},
		"ungranted image":        {CVEID: "CVE-2026-1", ImageRef: "registry.example/a:1"},
		"ungranted selector":     {CVEID: "CVE-2026-1", PackageID: "pkg:npm/p"},
		"ungranted public + img": {FindingID: "finding:a", ImageRef: "registry.example/a:1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := routeQueryer()
			_, _ = NewPostgresFindingStoreWithReadStore(fake, true).ExplainSupplyChainImpact(t.Context(), filter)
			if events := strings.Join(fake.events, ","); strings.Contains(events, "jit_off") {
				t.Fatalf("an explain switched jit off: %s", events)
			}
		})
	}
}
