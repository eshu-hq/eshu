// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// fixtureSnapshotExportEvidence names what the declaration in live suites
// proves: nothing about the build. The owned fixture image is usually an
// unpatched PostgreSQL 18.x, so declaring it exercises the #7957 guard
// plumbing and the snapshot-set mechanics under test, never build safety.
const fixtureSnapshotExportEvidence = "owned disposable test fixture build; exercises the #7957 guard plumbing only"

// declareFixtureSnapshotExport declares the fixture reader's exact
// server_version so live snapshot-set suites keep testing set mechanics on an
// undeclared fixture below 18.7. It reads the build from cfg.ReadDSN, which
// is the writer DSN for a same-primary pool and the first standby for a fleet.
func declareFixtureSnapshotExport(tb testing.TB, cfg *Config) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, cfg.ReadDSN)
	if err != nil {
		tb.Fatalf("read fixture server_version: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var serverVersion string
	if err := conn.QueryRow(ctx, "SELECT current_setting('server_version')").Scan(&serverVersion); err != nil {
		tb.Fatalf("read fixture server_version: %v", err)
	}
	for _, build := range cfg.SnapshotExportBuilds {
		if build.ServerVersion == serverVersion {
			return
		}
	}
	cfg.SnapshotExportBuilds = append(cfg.SnapshotExportBuilds, SnapshotExportBuild{ServerVersion: serverVersion, Evidence: fixtureSnapshotExportEvidence})
}
