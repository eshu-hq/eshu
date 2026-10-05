// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func TestSingleReaderMemberKeepsSnapshotAndFailsClosed(t *testing.T) {
	writer := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if writer == "" || reader == "" {
		t.Skip("owned primary and physical standby not configured")
	}
	readerEndpoint, err := parsePhysicalEndpoint(reader)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := json.Marshal([]ReaderMember{{
		ID: "read-0", Host: readerEndpoint.Host, Port: readerEndpoint.Port,
	}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return writer
		case "ESHU_POSTGRES_READ_DSN":
			return reader
		case "ESHU_POSTGRES_READ_MEMBERS":
			return string(inventory)
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	access, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Fatalf("open singleton fleet: %v (stale=%t unavailable=%t topology=%t deadline=%t)",
			err, errors.Is(err, ErrReaderStale), errors.Is(err, ErrReaderUnavailable),
			errors.Is(err, ErrWrongTopology), errors.Is(err, context.DeadlineExceeded))
	}
	t.Cleanup(func() {
		if closeErr := access.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	if len(access.readerMembers) != 1 || access.readerInventoryCount != 1 {
		t.Fatalf("qualified readers=%d inventory=%d, want 1/1", len(access.readerMembers), access.readerInventoryCount)
	}
	checked, err := access.ContextWithCheckpoint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	beginner, ok := access.Reader().(db.ReadSnapshotSetBeginner)
	if !ok {
		t.Fatal("singleton fleet did not advertise snapshot sets")
	}
	set, err := beginner.BeginReadOnlySnapshotSet(checked, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := set.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}()
	var snapshot, address string
	pids := map[int]bool{}
	for index := range 4 {
		queryer, readerErr := set.Reader(index)
		if readerErr != nil {
			t.Fatal(readerErr)
		}
		rows, queryErr := queryer.QueryContext(checked, "SELECT pg_current_snapshot()::text, inet_server_addr()::text, pg_backend_pid()")
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		if !rows.Next() {
			t.Fatal(rows.Err())
		}
		var gotSnapshot, gotAddress string
		var pid int
		if scanErr := rows.Scan(&gotSnapshot, &gotAddress, &pid); scanErr != nil {
			t.Fatal(scanErr)
		}
		if closeErr := rows.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if index > 0 && (gotSnapshot != snapshot || gotAddress != address) {
			t.Fatalf("snapshot or physical address changed within set")
		}
		snapshot, address = gotSnapshot, gotAddress
		pids[pid] = true
	}
	if len(pids) != 4 {
		t.Fatalf("snapshot set used %d distinct connections, want 4", len(pids))
	}
	if err := set.Close(); err != nil {
		t.Fatal(err)
	}
	if _, stats := access.Stats(); stats.InUse != 0 {
		t.Fatalf("singleton set leaked %d reader connections", stats.InUse)
	}
	access.readerMembers[0].incarnation = "stale-epoch"
	var localTopology memberLocalTopology
	if err := access.Ping(ctx); !errors.As(err, &localTopology) {
		t.Fatalf("readiness did not reject replaced member identity: %v", err)
	}
	if _, err := beginner.BeginReadOnlySnapshotSet(checked, 4); !errors.As(err, &localTopology) {
		t.Fatalf("snapshot set did not reject replaced member identity: %v", err)
	}
	if _, stats := access.Stats(); stats.InUse != 0 {
		t.Fatalf("failed singleton set leaked %d reader connections", stats.InUse)
	}
	primaryEndpoint, err := parsePhysicalEndpoint(writer)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ReadMembers[0].Host, cfg.ReadMembers[0].Port = primaryEndpoint.Host, primaryEndpoint.Port
	wrong, err := Open(ctx, cfg, nil)
	if wrong != nil {
		if closeErr := wrong.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	}
	if !errors.Is(err, ErrWrongTopology) {
		t.Fatalf("primary admitted as reader: %v", err)
	}
}

func TestSingleReaderMemberSnapshotSetupCost(t *testing.T) {
	writer := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if writer == "" || reader == "" {
		t.Skip("owned primary and physical standby not configured")
	}
	endpoint, err := parsePhysicalEndpoint(reader)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return writer
		case "ESHU_POSTGRES_READ_DSN":
			return reader
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	legacy, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := legacy.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	cfg.ReadMembers = []ReaderMember{{ID: "read-0", Host: endpoint.Host, Port: endpoint.Port}}
	fleet, err := Open(ctx, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := fleet.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})
	legacyCtx, err := legacy.ContextWithCheckpoint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fleetCtx, err := fleet.ContextWithCheckpoint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var startLSN, endLSN string
	if err := legacy.Writer().QueryRowContext(ctx, "SELECT pg_current_wal_insert_lsn()::text").Scan(&startLSN); err != nil {
		t.Fatal(err)
	}
	measure := func(access *Access, checked context.Context) time.Duration {
		t.Helper()
		beginner, ok := access.Reader().(db.ReadSnapshotSetBeginner)
		if !ok {
			t.Fatal("reader did not advertise snapshot sets")
		}
		started := time.Now()
		set, beginErr := beginner.BeginReadOnlySnapshotSet(checked, 4)
		if beginErr != nil {
			t.Fatal(beginErr)
		}
		if closeErr := set.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		return time.Since(started)
	}
	for range 2 {
		measure(legacy, legacyCtx)
		measure(fleet, fleetCtx)
	}
	baseline := make([]time.Duration, 0, 16)
	candidate := make([]time.Duration, 0, 16)
	for range 8 {
		baseline = append(baseline, measure(legacy, legacyCtx))
		candidate = append(candidate, measure(fleet, fleetCtx), measure(fleet, fleetCtx))
		baseline = append(baseline, measure(legacy, legacyCtx))
	}
	if err := legacy.Writer().QueryRowContext(ctx, "SELECT pg_current_wal_insert_lsn()::text").Scan(&endLSN); err != nil {
		t.Fatal(err)
	}
	if startLSN != endLSN {
		t.Fatalf("writer WAL changed during read-only A/B: %s -> %s", startLSN, endLSN)
	}
	t.Logf("interleaved baseline ns: %v", baseline)
	t.Logf("interleaved singleton fleet ns: %v", candidate)
	sort.Slice(baseline, func(i, j int) bool { return baseline[i] < baseline[j] })
	sort.Slice(candidate, func(i, j int) bool { return candidate[i] < candidate[j] })
	t.Logf("snapshot setup+close medians: legacy=%s singleton=%s",
		(baseline[7]+baseline[8])/2, (candidate[7]+candidate[8])/2)
}
