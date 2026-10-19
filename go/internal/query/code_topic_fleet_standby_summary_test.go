// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// codeTopicFleetProbeStubDriver answers the primary insert-LSN probe
// without a live database so the per-probe timeout contract stays provable
// in a focused unit run. It lives here, not in the live enrollment file,
// because it is hermetic: the readiness ledger enrolls only genuine live
// proofs per file.
func init() {
	sql.Register("code-topic-fleet-probe-stub", codeTopicFleetProbeStubDriver{})
}

type codeTopicFleetProbeStubDriver struct{}

func (codeTopicFleetProbeStubDriver) Open(string) (driver.Conn, error) {
	return codeTopicFleetProbeStubConn{}, nil
}

type codeTopicFleetProbeStubConn struct{}

func (codeTopicFleetProbeStubConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("code-topic-fleet-probe-stub: prepare unsupported")
}

func (codeTopicFleetProbeStubConn) Close() error { return nil }

func (codeTopicFleetProbeStubConn) Begin() (driver.Tx, error) {
	return nil, errors.New("code-topic-fleet-probe-stub: begin unsupported")
}

func (codeTopicFleetProbeStubConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	return &codeTopicFleetProbeStubRows{}, nil
}

type codeTopicFleetProbeStubRows struct {
	done bool
}

func (r *codeTopicFleetProbeStubRows) Columns() []string {
	return []string{"pg_current_wal_insert_lsn"}
}

func (r *codeTopicFleetProbeStubRows) Close() error { return nil }

func (r *codeTopicFleetProbeStubRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = "0/1A2B3C"
	return nil
}

// TestCodeTopicFleetStandbySummaryPerProbeTimeout pins the #8579 P2 fix: a
// hung reader probe must not starve the primary insert-LSN probe. The
// reader DSN points at a local TCP acceptor that never answers (a stalled
// standby), while the stub primary answers instantly; the summary must
// still report the primary LSN under short per-probe timeouts.
func TestCodeTopicFleetStandbySummaryPerProbeTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen black-hole probe port: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(io.Discard, conn)
			}()
		}
	}()
	port, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("black-hole listener addr %v, want TCP", listener.Addr())
	}
	readerDSN := "postgres://127.0.0.1:" + strconv.Itoa(port.Port) + "/postgres?sslmode=disable"
	primary, err := sql.Open("code-topic-fleet-probe-stub", "")
	if err != nil {
		t.Fatalf("open stub primary: %v", err)
	}
	t.Cleanup(func() { _ = primary.Close() })
	started := time.Now()
	got := codeTopicFleetStandbySummaryWithTimeout(context.Background(), readerDSN, primary, 300*time.Millisecond)
	if elapsed := time.Since(started); elapsed >= 5*time.Second {
		t.Fatalf("probes took %s, want bounded per-probe timeouts", elapsed)
	}
	if !strings.Contains(got, "standby-recovery=unknown") {
		t.Fatalf("hung reader probe should stay unknown: got %q", got)
	}
	if !strings.Contains(got, "insert-lsn=0/1A2B3C") {
		t.Fatalf("starved primary probe: got %q", got)
	}
}
