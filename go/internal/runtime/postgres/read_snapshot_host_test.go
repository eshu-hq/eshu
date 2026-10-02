// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/jackc/pgx/v5"
)

func TestReadSnapshotSetCapabilityRequiresOnePhysicalReaderHost(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		wantSets bool
	}{
		{name: "single host", dsn: "host=reader-a user=proof dbname=eshu sslmode=disable", wantSets: true},
		{name: "fallback host", dsn: "host=reader-a,reader-b user=proof dbname=eshu sslmode=disable", wantSets: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := pgx.ParseConfig(tc.dsn)
			if err != nil {
				t.Fatal(err)
			}
			access := &Access{readerHasFallbacks: len(cfg.Fallbacks) > 0}
			reader := access.Reader()
			if _, ok := reader.(db.ReadSnapshotSetBeginner); ok != tc.wantSets {
				t.Fatalf("snapshot set capability=%t, want %t", ok, tc.wantSets)
			}
		})
	}
}

func TestReadSnapshotSetDirectMultiHostCallRejectsBeforeBorrow(t *testing.T) {
	access := &Access{readerHasFallbacks: true}
	_, err := (fencedQueryer{access: access}).BeginReadOnlySnapshotSet(context.Background(), 1)
	if !errors.Is(err, errSnapshotSetMultiHost) {
		t.Fatalf("direct multi-host snapshot set error=%v, want unsupported-host error", err)
	}
}
