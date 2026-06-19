package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestSharedIntentStoreClaimPartitionLeaseBlocksActivePartitionCountRescale(t *testing.T) {
	t.Parallel()

	store := NewSharedIntentStore(partitionRescaleGuardDB{})
	claimed, err := store.ClaimPartitionLease(
		context.Background(),
		string(reducer.DomainCodeCalls),
		0,
		8,
		"new-worker",
		30*time.Second,
	)
	if err != nil {
		t.Fatalf("ClaimPartitionLease: %v", err)
	}
	if claimed {
		t.Fatal("ClaimPartitionLease claimed new partition count while old count lease was active")
	}
}

type partitionRescaleGuardDB struct{}

func (partitionRescaleGuardDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected exec")
}

func (partitionRescaleGuardDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	if !strings.Contains(query, "pg_advisory_xact_lock") ||
		!strings.Contains(query, "shared_projection_partition_leases") ||
		!strings.Contains(query, "hashtext($1)") ||
		!strings.Contains(query, "partition_count <> $3") ||
		!strings.Contains(query, "lease_owner IS NOT NULL") ||
		!strings.Contains(query, "lease_expires_at > $6") {
		return &leaseResultRows{
			data: [][]any{{args[0].(string)}},
			idx:  -1,
		}, nil
	}
	return &leaseResultRows{idx: -1}, nil
}
