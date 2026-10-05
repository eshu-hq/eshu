// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

func TestReaderMemberLossMarkerOnlyForEstablishedFleetTransportLoss(t *testing.T) {
	marker := interface{ ReaderMemberLost() bool }(nil)
	for _, tc := range []struct {
		name  string
		err   error
		fleet bool
		want  bool
	}{
		{"fleet EOF", io.EOF, true, true},
		{"fleet bad connection", driver.ErrBadConn, true, true},
		{"fleet admin shutdown", fmt.Errorf("rows failed: %w", &pgconn.PgError{Code: "57P01"}), true, true},
		{"fleet crash shutdown", &pgconn.PgError{Code: "57P02"}, true, true},
		{"legacy admin shutdown", &pgconn.PgError{Code: "57P01"}, false, false},
		{"fleet query canceled", &pgconn.PgError{Code: "57014"}, true, false},
		{"fleet too many connections", &pgconn.PgError{Code: "53300"}, true, false},
		{"fleet invalid password", &pgconn.PgError{Code: "28P01"}, true, false},
		{"fleet insufficient privilege", &pgconn.PgError{Code: "42501"}, true, false},
		{"fleet canceled shutdown", errors.Join(context.Canceled, &pgconn.PgError{Code: "57P01"}), true, false},
		{"legacy EOF", io.EOF, false, false},
		{"fleet canceled", context.Canceled, true, false},
		{"fleet SQL failure", errors.New("invalid SQL"), true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := memberQueryFailure(tc.err, tc.fleet)
			if marked := errors.As(got, &marker) && marker.ReaderMemberLost(); marked != tc.want {
				t.Fatalf("member-loss classification=%t, want %t", marked, tc.want)
			}
		})
	}
}

func TestReaderMemberDirectAddressCheckRejectsServiceAddress(t *testing.T) {
	actual := "10.2.0.11" // Server reports its own accepted-connection address.
	direct := []net.IP{net.ParseIP(actual)}
	service := []net.IP{net.ParseIP("10.96.0.80")}
	if !addressMatches(actual, direct) || addressMatches(actual, service) {
		t.Fatal("direct address check did not distinguish member from Service VIP")
	}
}

func TestLoadConfigReaderMembersAreCredentialFreeAndBounded(t *testing.T) {
	writer := "postgres://proof:secret@writer/eshu"
	reader := "postgres://proof:secret@reader-service/eshu?sslmode=disable"
	for _, tc := range []struct {
		name, inventory string
		valid           bool
	}{
		{"two physical members", `[{"id":"a","host":"reader-a","port":5432},{"id":"b","host":"reader-b","port":5432}]`, true},
		{"duplicate ID", `[{"id":"a","host":"reader-a","port":5432},{"id":"a","host":"reader-b","port":5432}]`, false},
		{"duplicate endpoint", `[{"id":"a","host":"reader-a","port":5432},{"id":"b","host":"reader-a","port":5432}]`, false},
		{"credentials", `[{"id":"a","host":"proof:secret@reader-a","port":5432}]`, false},
		{"single member", `[{"id":"a","host":"reader-a","port":5432}]`, true},
		{"empty inventory", `[]`, false},
		{"insufficient budget", `[{"id":"a","host":"reader-a","port":5432},{"id":"b","host":"reader-b","port":5432},{"id":"c","host":"reader-c","port":5432},{"id":"d","host":"reader-d","port":5432}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadConfig(func(key string) string {
				switch key {
				case "ESHU_POSTGRES_DSN":
					return writer
				case "ESHU_POSTGRES_READ_DSN":
					return reader
				case "ESHU_POSTGRES_READ_MEMBERS":
					return tc.inventory
				default:
					return ""
				}
			})
			if (err == nil) != tc.valid {
				t.Fatalf("LoadConfig error=%v, valid=%t", err, tc.valid)
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatalf("credential leaked: %v", err)
			}
			if tc.valid && (len(cfg.ReadMembers) == 0 || cfg.ReadMembers[0].ID != "a" || cfg.ReadMaxOpenConns+cfg.WriterMaxOpenConns != 30) {
				t.Fatalf("member config or pool cap: %+v", cfg)
			}
		})
	}
}

func TestLoadConfigReaderMembersIPv6Hosts(t *testing.T) {
	for _, tc := range []struct {
		name, inventory string
		valid           bool
	}{
		{"IPv4 and DNS", `[{"id":"a","host":"127.0.0.1","port":5432},{"id":"b","host":"reader-b","port":5432}]`, true},
		{"bare IPv6", `[{"id":"a","host":"::1","port":5432},{"id":"b","host":"2001:db8::2","port":5432}]`, true},
		{"scoped IPv6", `[{"id":"a","host":"fe80::1%eth0","port":5432},{"id":"b","host":"2001:db8::2","port":5432}]`, true},
		{"bracketed IPv6", `[{"id":"a","host":"[::1]","port":5432},{"id":"b","host":"2001:db8::2","port":5432}]`, false},
		{"host and port", `[{"id":"a","host":"reader-a:5432","port":5432},{"id":"b","host":"2001:db8::2","port":5432}]`, false},
		{"duplicate IPv6 spelling", `[{"id":"a","host":"2001:0db8::2","port":5432},{"id":"b","host":"2001:db8::2","port":5432}]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadConfig(func(key string) string {
				switch key {
				case "ESHU_POSTGRES_DSN":
					return "postgres://proof:secret@writer/eshu"
				case "ESHU_POSTGRES_READ_DSN":
					return "postgres://proof:secret@reader-service/eshu?sslmode=disable"
				case "ESHU_POSTGRES_READ_MEMBERS":
					return tc.inventory
				default:
					return ""
				}
			})
			if (err == nil) != tc.valid {
				t.Fatalf("LoadConfig error=%v, valid=%t", err, tc.valid)
			}
		})
	}
}

func TestLoadConfigReaderMembersRejectAmbiguousTransport(t *testing.T) {
	inventory := `[{"id":"a","host":"reader-a","port":5432},{"id":"b","host":"reader-b","port":5432}]`
	for _, readDSN := range []string{
		"host=reader-service user=proof dbname=eshu sslmode=prefer",
		"host=reader-a,reader-b user=proof dbname=eshu sslmode=disable",
		"host=writer user=proof dbname=eshu sslmode=disable",
	} {
		t.Run(readDSN, func(t *testing.T) {
			_, err := LoadConfig(func(key string) string {
				switch key {
				case "ESHU_POSTGRES_DSN":
					return "host=writer user=proof dbname=eshu sslmode=disable"
				case "ESHU_POSTGRES_READ_DSN":
					return readDSN
				case "ESHU_POSTGRES_READ_MEMBERS":
					return inventory
				default:
					return ""
				}
			})
			if err == nil {
				t.Fatal("ambiguous fleet transport was accepted")
			}
		})
	}
}

func TestReaderMembersSnapshotSetsStayOnOnePhysicalReader(t *testing.T) {
	writer := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	second := os.Getenv("ESHU_READER_TEST_SECOND_READER_DSN")
	if writer == "" || reader == "" || second == "" {
		t.Skip("owned primary and two physical standbys not configured")
	}
	memberA, err := parsePhysicalEndpoint(reader)
	if err != nil {
		t.Fatal(err)
	}
	memberB, err := parsePhysicalEndpoint(second)
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
	cfg.ReadMembers = []ReaderMember{{ID: "a", Host: memberA.Host, Port: memberA.Port}, {ID: "b", Host: memberB.Host, Port: memberB.Port}}
	cfg.ReadMaxOpenConns = 8
	cfg.ReadMaxIdleConns = 8
	cfg.ReplayTimeout = 150 * time.Millisecond
	access, err := Open(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = access.Close() })
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	beginner, ok := access.Reader().(db.ReadSnapshotSetBeginner)
	if !ok {
		t.Fatal("fleet mode did not advertise snapshot sets")
	}
	seen := map[string]bool{}
	for range 4 {
		set, err := beginner.BeginReadOnlySnapshotSet(ctx, 4)
		if err != nil {
			t.Fatal(err)
		}
		var host, snapshot string
		for i := range 4 {
			queryer, err := set.Reader(i)
			if err != nil {
				t.Fatal(err)
			}
			var gotHost, gotSnapshot string
			rows, err := queryer.QueryContext(ctx, "SELECT inet_server_addr()::text, pg_current_snapshot()::text")
			if err != nil {
				t.Fatal(err)
			}
			if !rows.Next() {
				t.Fatal(rows.Err())
			}
			if err := rows.Scan(&gotHost, &gotSnapshot); err != nil {
				t.Fatal(err)
			}
			if err := rows.Close(); err != nil {
				t.Fatal(err)
			}
			if i > 0 && (gotHost != host || gotSnapshot != snapshot) {
				t.Fatalf("mixed physical reader or snapshot: %q/%q != %q/%q", gotHost, gotSnapshot, host, snapshot)
			}
			host, snapshot = gotHost, gotSnapshot
		}
		seen[host] = true
		if err := set.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 2 {
		t.Fatalf("sets distributed over %d members, want 2", len(seen))
	}
	if _, readerStats := access.Stats(); readerStats.InUse != 0 || readerStats.MaxOpenConnections != 8 {
		t.Fatalf("aggregate reader pool stats=%+v", readerStats)
	}
	first, err := beginner.BeginReadOnlySnapshotSet(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	secondSet, err := beginner.BeginReadOnlySnapshotSet(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer secondSet.Close()
	if _, stats := access.Stats(); stats.InUse != 8 || stats.MaxOpenConnections != 8 {
		t.Fatalf("concurrent set reader budget=%+v", stats)
	}
	shortCtx, cancel := context.WithTimeout(ctx, 25*time.Millisecond)
	defer cancel()
	if _, err := beginner.BeginReadOnlySnapshotSet(shortCtx, 4); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("over-cap canceled reservation=%v", err)
	}
	if err := secondSet.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if _, stats := access.Stats(); stats.InUse != 0 {
		t.Fatalf("canceled reservation leaked readers: %+v", stats)
	}
	if err := access.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	// A changed incarnation is rejected before business SQL. The whole
	// reservation is discarded and rebuilt on B, never half-imported.
	oldIncarnation := access.readerMembers[0].incarnation
	access.readerMembers[0].incarnation = "replaced"
	access.nextReader.Store(0)
	replacedSet, err := beginner.BeginReadOnlySnapshotSet(ctx, 4)
	if err != nil {
		t.Fatalf("whole-set retry after member replacement: %v", err)
	}
	if _, stats := access.Stats(); stats.InUse != 4 {
		t.Fatalf("replacement retry leaked partial readers: %+v", stats)
	}
	if err := replacedSet.Close(); err != nil {
		t.Fatal(err)
	}
	access.readerMembers[0].incarnation = oldIncarnation
	// A refused dial models a lost Pod without treating a deliberately closed
	// in-process pool as a PostgreSQL transport failure. Readiness and a
	// complete new snapshot remain available on the other qualified member.
	if err := access.readerMembers[1].pool.Close(); err != nil {
		t.Fatal(err)
	}
	deadMember := memberB.Copy()
	deadMember.DialFunc = func(context.Context, string, string) (net.Conn, error) {
		return nil, syscall.ECONNREFUSED
	}
	access.readerMembers[1].pool = stdlib.OpenDB(*deadMember)
	access.nextReader.Store(1)
	lostSet, err := beginner.BeginReadOnlySnapshotSet(ctx, 4)
	if err != nil {
		t.Fatalf("whole-set retry after member loss: %v", err)
	}
	if err := lostSet.Close(); err != nil {
		t.Fatal(err)
	}
	if err := access.Ping(ctx); err != nil {
		t.Fatalf("readiness with one qualified member: %v", err)
	}
}

func TestReaderMembersRejectPrimaryAsStandby(t *testing.T) {
	writer := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	reader := os.Getenv("ESHU_READER_TEST_READER_DSN")
	if writer == "" || reader == "" {
		t.Skip("owned primary and physical standby not configured")
	}
	primary, err := parsePhysicalEndpoint(writer)
	if err != nil {
		t.Fatal(err)
	}
	standby, err := parsePhysicalEndpoint(reader)
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
	cfg.ReadMembers = []ReaderMember{{ID: "standby", Host: standby.Host, Port: standby.Port}, {ID: "wrong", Host: primary.Host, Port: primary.Port}}
	access, err := Open(context.Background(), cfg, nil)
	if access != nil {
		_ = access.Close()
	}
	if !errors.Is(err, ErrWrongTopology) {
		t.Fatalf("primary member error=%v, want wrong topology", err)
	}
}

// openFleetRegressionAccess uses only an explicitly owned primary and two
// standbys. It does not create or mutate database state.
func openFleetRegressionAccess(t *testing.T, replayTimeout time.Duration) *Access {
	t.Helper()
	writer := os.Getenv("ESHU_READER_TEST_WRITER_DSN")
	first := os.Getenv("ESHU_READER_TEST_READER_DSN")
	second := os.Getenv("ESHU_READER_TEST_SECOND_READER_DSN")
	if writer == "" || first == "" || second == "" {
		t.Skip("owned primary and two physical standbys not configured")
	}
	firstEndpoint, err := parsePhysicalEndpoint(first)
	if err != nil {
		t.Fatal(err)
	}
	secondEndpoint, err := parsePhysicalEndpoint(second)
	if err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}
	cfg.ReadMembers = []ReaderMember{
		{ID: "a", Host: firstEndpoint.Host, Port: firstEndpoint.Port},
		{ID: "b", Host: secondEndpoint.Host, Port: secondEndpoint.Port},
	}
	cfg.ReadMaxOpenConns = 8
	cfg.ReadMaxIdleConns = 8
	cfg.ReplayTimeout = replayTimeout
	access, err := Open(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := access.Close(); err != nil {
			t.Error(err)
		}
	})
	return access
}

func TestReaderMembersSkipSaturatedMemberBeforeWaiting(t *testing.T) {
	access := openFleetRegressionAccess(t, 400*time.Millisecond)
	ctx, err := access.ContextWithCheckpoint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	beginner := access.Reader().(db.ReadSnapshotSetBeginner)
	access.nextReader.Store(0)
	first, err := beginner.BeginReadOnlySnapshotSet(ctx, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	if got := access.readerMembers[0].pool.Stats().InUse; got != 4 {
		t.Fatalf("first member holds %d connections, want 4", got)
	}
	if got := access.readerMembers[1].pool.Stats().InUse; got != 0 {
		t.Fatalf("second member holds %d connections, want idle", got)
	}
	access.nextReader.Store(0)
	started := time.Now()
	second, err := beginner.BeginReadOnlySnapshotSet(ctx, 4)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if got := access.readerMembers[1].pool.Stats().InUse; got != 4 {
		t.Fatalf("second member holds %d connections, want 4", got)
	}
	if elapsed >= 200*time.Millisecond {
		t.Fatalf("idle peer selected after %s; waited behind saturated member", elapsed)
	}
}

func TestReaderMembersPingRequiresFrozenMemberIdentity(t *testing.T) {
	access := openFleetRegressionAccess(t, 400*time.Millisecond)
	if err := access.Ping(context.Background()); err != nil {
		t.Fatalf("healthy fleet ping: %v", err)
	}
	access.readerMembers[0].incarnation = "replaced"
	if err := access.Ping(context.Background()); err != nil {
		t.Fatalf("one qualified member should keep readiness: %v", err)
	}
	access.readerMembers[1].incarnation = "replaced"
	if err := access.Ping(context.Background()); err == nil {
		t.Fatal("readiness accepted two members that no longer match frozen identity")
	}
}

func TestMemberOrderCounterWrapKeepsIndexesInBounds(t *testing.T) {
	access := &Access{readerMembers: []physicalReaderMember{
		{maxOpen: 4}, {maxOpen: 4}, {maxOpen: 4},
	}}
	access.nextReader.Store(^uint64(0))
	order := access.memberOrder(4)
	if len(order) != 3 || order[0] != 0 || order[1] != 1 || order[2] != 2 {
		t.Fatalf("member order after counter wrap = %v, want [0 1 2]", order)
	}
}
