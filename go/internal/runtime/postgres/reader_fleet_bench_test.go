// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/jackc/pgx/v5/stdlib"
)

func benchmarkFleetAccess(b *testing.B) (*Access, context.Context) {
	b.Helper()
	writer := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	first := os.Getenv("ESHU_READER_TEST_READER_DSN")
	second := os.Getenv("ESHU_READER_TEST_SECOND_READER_DSN")
	if writer == "" || first == "" || second == "" {
		b.Skip("owned primary and two physical standbys not configured")
	}
	firstEndpoint, err := parsePhysicalEndpoint(first)
	if err != nil {
		b.Fatal(err)
	}
	secondEndpoint, err := parsePhysicalEndpoint(second)
	if err != nil {
		b.Fatal(err)
	}
	cfg, err := LoadConfig(func(key string) string {
		switch key {
		case "ESHU_POSTGRES_DSN":
			return writer
		case "ESHU_POSTGRES_READ_DSN":
			return first
		default:
			return ""
		}
	})
	if err != nil {
		b.Fatal(err)
	}
	cfg.ReadMembers = []ReaderMember{
		{ID: "a", Host: firstEndpoint.Host, Port: firstEndpoint.Port},
		{ID: "b", Host: secondEndpoint.Host, Port: secondEndpoint.Port},
	}
	cfg.ReadMaxOpenConns = 8
	cfg.ReadMaxIdleConns = 8
	cfg.ReplayTimeout = 2 * time.Second
	access, err := Open(context.Background(), cfg, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() {
		if err := access.Close(); err != nil {
			b.Error(err)
		}
	})
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	return access, ctx
}

// BenchmarkFleetSnapshotSetup measures the complete four-connection snapshot
// setup and close against an owned primary and two physical standbys.
func BenchmarkFleetSnapshotSetup(b *testing.B) {
	access, ctx := benchmarkFleetAccess(b)
	beginner := access.Reader().(db.ReadSnapshotSetBeginner)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		set, err := beginner.BeginReadOnlySnapshotSet(ctx, 4)
		if err != nil {
			b.Fatal(err)
		}
		if err := set.Close(); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

// BenchmarkFleetStalledFirstMember measures sequential failover when the first
// standby's BEGIN receives no reply. The second member remains physical.
func BenchmarkFleetStalledFirstMember(b *testing.B) {
	access, ctx := benchmarkFleetAccess(b)
	first := os.Getenv("ESHU_READER_TEST_READER_DSN")
	config, err := parsePhysicalEndpoint(first)
	if err != nil {
		b.Fatal(err)
	}
	member := &access.readerMembers[0]
	config.RuntimeParams["default_transaction_read_only"] = "on"
	config.ValidateConnect = memberValidator(access.lineage.identity().physicalIdentity, member.incarnation, member.addresses)
	baseDial := config.DialFunc
	if baseDial == nil {
		baseDial = (&net.Dialer{}).DialContext
	}
	var opened, closed atomic.Int64
	stageSeen := make(chan struct{}, 1)
	config.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, dialErr := baseDial(ctx, network, address)
		if dialErr != nil {
			return nil, dialErr
		}
		opened.Add(1)
		return &stalledSetupConn{Conn: conn, statement: []byte("BEGIN"), stageSeen: stageSeen, closed: &closed}, nil
	}
	if err := member.pool.Close(); err != nil {
		b.Fatal(err)
	}
	member.pool = stdlib.OpenDB(*config)
	member.pool.SetMaxOpenConns(member.maxOpen)
	member.pool.SetMaxIdleConns(0)
	beginner := access.Reader().(db.ReadSnapshotSetBeginner)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		access.nextReader.Store(0)
		select {
		case <-stageSeen:
		default:
		}
		set, err := beginner.BeginReadOnlySnapshotSet(ctx, 4)
		if err != nil {
			b.Fatal(err)
		}
		if err := set.Close(); err != nil {
			b.Fatal(err)
		}
		select {
		case <-stageSeen:
		default:
			b.Fatal("first member did not reach stalled BEGIN")
		}
	}
	b.StopTimer()
	reserved, waiting := access.allocator.pressure()
	for index := range reserved {
		if reserved[index] != 0 || waiting[index] != 0 {
			b.Fatalf("member %d reserved=%d waiting=%d after setup", index, reserved[index], waiting[index])
		}
	}
	if opened.Load() == 0 || closed.Load() == 0 || member.pool.Stats().InUse != 0 {
		b.Fatalf("stalled member opened=%d closed=%d in_use=%d", opened.Load(), closed.Load(), member.pool.Stats().InUse)
	}
}
