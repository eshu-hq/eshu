// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/content"
)

// TestContentWriterWriteDerivesConfigArtifactsLive is the built-path proof
// (#8155 PR3b-2c-2): the startup helper builds a deriver over the bootstrapped
// schema, Write derives the written repository, derives_total says
// write/ok, and the state row carries extractor version 1 (arb-8155-2c: the
// wiring supplies contentartifacts.ExtractorVersion, no other constant).
//
// Locally:
//
//	ESHU_POSTGRES_TEST_DSN=postgresql://postgres:eshu8155@localhost:55453/postgres?sslmode=disable \
//	go test ./internal/storage/postgres \
//	  -run TestContentWriterWriteDerivesConfigArtifactsLive -count=1 -v
func TestContentWriterWriteDerivesConfigArtifactsLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	db, _ := openServiceLineageSchemaLive(ctx, t, "eshu_8155_cfgart")
	if err := ApplyBootstrap(ctx, SQLDB{DB: db}); err != nil {
		t.Fatalf("ApplyBootstrap: %v", err)
	}
	pool := SQLDB{DB: db}

	deriver, err := NewConfigArtifactWriteDeriver(ctx, pool, func(string) string { return "" }, nil)
	if err != nil {
		t.Fatalf("NewConfigArtifactWriteDeriver() error = %v", err)
	}
	if deriver == nil {
		t.Fatal("NewConfigArtifactWriteDeriver() = nil, want a deriver on a bootstrapped schema")
	}

	instruments, reader := newDeriveMeter(t)
	writer := NewContentWriter(pool).WithInstruments(instruments).WithConfigArtifactDeriver(deriver)
	mat := content.Materialization{
		RepoID:       "repo-live-1",
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		Records: []content.Record{
			{Path: "main.tf", Body: "resource \"aws_instance\" \"web\" {\n  ami = \"ami-123\"\n}\n"},
			{Path: "docker-compose.yml", Body: "services:\n  web:\n    image: example/web:1\n"},
		},
	}
	if _, err := writer.Write(ctx, mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	counts, _ := configArtifactDeriveSeries(t, reader)
	if counts[[2]string{"write", "ok"}] != 1 {
		t.Fatalf("derive series = %v, want one write/ok", counts)
	}
	var extractorVersion, rowCount int
	var contentVersion int64
	if err := db.QueryRowContext(ctx,
		`SELECT extractor_version, row_count, content_version FROM config_artifact_repo_state WHERE repo_id = $1`,
		"repo-live-1",
	).Scan(&extractorVersion, &rowCount, &contentVersion); err != nil {
		t.Fatalf("read state row: %v", err)
	}
	if extractorVersion != 1 {
		t.Fatalf("state extractor_version = %d, want 1", extractorVersion)
	}
	if contentVersion < 1 {
		t.Fatalf("state content_version = %d, want the fence-bumped version of this Write", contentVersion)
	}
	t.Logf("state: extractor_version=%d row_count=%d content_version=%d", extractorVersion, rowCount, contentVersion)
}
