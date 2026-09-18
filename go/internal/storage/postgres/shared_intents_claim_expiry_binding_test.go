// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// claimArgsRecordingDB captures the claim query text and bound args without
// touching a database, so the test proves what ClaimPartitionLease binds for
// $5 rather than what any one backend does with it.
type claimArgsRecordingDB struct {
	query string
	args  []any
}

func (f *claimArgsRecordingDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, nil
}

func (f *claimArgsRecordingDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	f.query = query
	f.args = args
	return &leaseResultRows{data: [][]any{{"expiry-proof-domain"}}, idx: -1}, nil
}

// TestClaimPartitionLeaseBindsTTLForServerSideExpiry is the regression test
// for #6760: the Ifá killworker cell kills a reducer while its partition
// lease claims are parked behind the test holder's advisory lock, and the
// capture step requires lease_expires_at more than 4s in the future. When the
// expiry is bound client-side at call time, every second spent blocked burns
// TTL before the row exists, so post-release orphan commits land already
// stale and the capture rejects them. Binding the TTL itself and computing
// the expiry server-side at commit keeps the full TTL no matter how long the
// claim waited.
func TestClaimPartitionLeaseBindsTTLForServerSideExpiry(t *testing.T) {
	t.Parallel()

	recording := &claimArgsRecordingDB{}
	store := NewSharedIntentStore(recording)

	const ttl = 30 * time.Second
	claimed, err := store.ClaimPartitionLease(context.Background(), "expiry-proof-domain", 0, 4, "owner-a", ttl)
	if err != nil {
		t.Fatalf("ClaimPartitionLease error = %v", err)
	}
	if !claimed {
		t.Fatal("ClaimPartitionLease claimed = false, want true")
	}

	if len(recording.args) != 6 {
		t.Fatalf("claim bound %d args, want 6 (domain, partition, count, owner, ttl, now)", len(recording.args))
	}
	ttlSeconds, ok := recording.args[4].(float64)
	if !ok {
		t.Fatalf("claim $5 has type %T, want TTL seconds (float64) bound for server-side expiry", recording.args[4])
	}
	if ttlSeconds != ttl.Seconds() {
		t.Fatalf("claim $5 = %v seconds, want TTL %v", ttlSeconds, ttl.Seconds())
	}
	if !strings.Contains(recording.query, "clock_timestamp()") {
		t.Fatalf("claim SQL does not compute lease_expires_at server-side; query:\n%s", recording.query)
	}
}
