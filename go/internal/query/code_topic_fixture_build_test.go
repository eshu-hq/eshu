// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	runtimepostgres "github.com/eshu-hq/eshu/go/internal/runtime/postgres"
)

// codeTopicFixtureEvidence states what declaring a live fixture's build proves:
// nothing about build safety. The CI standby is an unpatched mirror of
// postgres:18.6-bookworm, so the declaration only lets the #7957 guard admit
// it so these suites keep exercising the shared-snapshot plumbing.
const codeTopicFixtureEvidence = "unpatched CI mirror of postgres:18.6-bookworm (or an owned local fixture); declaration exercises the #7957 guard plumbing only"

// codeTopicFixtureSnapshotExportBuilds reads the fixture reader's exact
// server_version and returns the one declaration that admits it.
func codeTopicFixtureSnapshotExportBuilds(t *testing.T, dsn string) []runtimepostgres.SnapshotExportBuild {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("read fixture server_version: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	var serverVersion string
	if err := conn.QueryRow(ctx, "SELECT current_setting('server_version')").Scan(&serverVersion); err != nil {
		t.Fatalf("read fixture server_version: %v", err)
	}
	return []runtimepostgres.SnapshotExportBuild{{ServerVersion: serverVersion, Evidence: codeTopicFixtureEvidence}}
}

// codeTopicFixtureSnapshotExportEnv encodes the declaration as the
// ESHU_POSTGRES_READ_SNAPSHOT_EXPORT_BUILDS value LoadConfig reads.
func codeTopicFixtureSnapshotExportEnv(t *testing.T, dsn string) string {
	t.Helper()
	encoded, err := json.Marshal(codeTopicFixtureSnapshotExportBuilds(t, dsn))
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
