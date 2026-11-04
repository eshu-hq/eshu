// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestWorkloadCandidateProbeRefusedDialClassifiesAsConnection pins F4 with a
// real driver error: the production probe, run through FactStore against a
// port nothing listens on, fails with a pgx connect error that carries no
// SQLSTATE. A Postgres outage must land under cause_class=connection, not
// other.
func TestWorkloadCandidateProbeRefusedDialClassifiesAsConnection(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a local port: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release the local port: %v", err)
	}

	db, err := sql.Open("pgx", fmt.Sprintf("postgres://eshu@%s/eshu?sslmode=disable&connect_timeout=5", addr))
	if err != nil {
		t.Fatalf("open pgx: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	store := postgres.NewFactStore(postgres.SQLDB{DB: db})
	_, probeErr := store.ScopeMayHoldWorkloadCandidates(ctx, "scope-1", "gen-1")
	if probeErr == nil {
		t.Fatal("probe error = nil, want a refused-dial failure")
	}
	var connectErr *pgconn.ConnectError
	if !errors.As(probeErr, &connectErr) {
		t.Fatalf("probe error = %T %v, want a pgconn.ConnectError from a refused dial", probeErr, probeErr)
	}

	cause, sqlState := reducer.ClassifyWorkloadCandidateProbeErrorForTest(probeErr)
	if cause != telemetry.WorkloadCandidateProbeCauseConnection || sqlState != "" {
		t.Fatalf("classify(%v) = (%q, %q), want (%q, \"\")", probeErr, cause, sqlState,
			telemetry.WorkloadCandidateProbeCauseConnection)
	}
}
