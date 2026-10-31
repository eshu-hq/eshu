// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestScopeMayHoldWorkloadCandidatesLive is the #9697 live proof for the
// workload bulk-load pre-gate probe, mirroring
// TestListActiveContainerImageCIFactsOwnerScopedQueryLive's isolated-schema
// pattern: against a real Postgres planner the probe reports true only for
// the scope generation whose repository identity joins to a signal-bearing
// file fact, and false for every candidate-free shape (repo-only,
// empty-stages, signal-free parsed payload, foreign repo_id, nameless
// repository, and the bare-Jenkinsfile-path exception that loads normally).
func TestScopeMayHoldWorkloadCandidatesLive(t *testing.T) {
	const schema = "eshu_9697_candidate_precondition_live"

	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live #9697 candidate-precondition proof")
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	adminDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = adminDB.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	if _, err := adminDB.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE; CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create isolated proof schema: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		if _, err := adminDB.ExecContext(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE"); err != nil {
			t.Errorf("drop isolated proof schema: %v", err)
		}
	})

	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse Postgres DSN: %v", err)
	}
	query := parsedDSN.Query()
	query.Set("search_path", schema)
	parsedDSN.RawQuery = query.Encode()
	db, err := sql.Open("pgx", parsedDSN.String())
	if err != nil {
		t.Fatalf("open isolated Postgres schema: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.ExecContext(ctx, `
CREATE TABLE ingestion_scopes (
    scope_id TEXT PRIMARY KEY,
    scope_kind TEXT NOT NULL,
    source_system TEXT NOT NULL,
    source_key TEXT NOT NULL,
    parent_scope_id TEXT NULL,
    collector_kind TEXT NOT NULL,
    partition_key TEXT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    active_generation_id TEXT NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE scope_generations (
    generation_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    trigger_kind TEXT NOT NULL,
    freshness_hint TEXT NULL,
    source_commit_sha TEXT NULL,
    is_delta BOOLEAN NOT NULL DEFAULT false,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL,
    activated_at TIMESTAMPTZ NULL,
    superseded_at TIMESTAMPTZ NULL,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE TABLE fact_records (
    fact_id TEXT PRIMARY KEY,
    scope_id TEXT NOT NULL REFERENCES ingestion_scopes(scope_id) ON DELETE CASCADE,
    generation_id TEXT NOT NULL REFERENCES scope_generations(generation_id) ON DELETE CASCADE,
    fact_kind TEXT NOT NULL,
    stable_fact_key TEXT NOT NULL,
    schema_version TEXT NOT NULL DEFAULT '0.0.0',
    collector_kind TEXT NOT NULL DEFAULT 'unknown',
    fencing_token BIGINT NOT NULL DEFAULT 0,
    source_confidence TEXT NOT NULL DEFAULT 'unknown',
    source_system TEXT NOT NULL,
    source_fact_key TEXT NOT NULL,
    source_uri TEXT NULL,
    source_record_id TEXT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    ingested_at TIMESTAMPTZ NOT NULL,
    is_tombstone BOOLEAN NOT NULL DEFAULT FALSE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb
);

INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES
    ('scope-signals', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-a'),
    ('scope-empty-stages', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-b'),
    ('scope-repo-only', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-c'),
    ('scope-foreign', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-d'),
    ('scope-noname', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-e'),
    ('scope-jenkins-path', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-f'),
    ('scope-signal-free', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-g');

INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES
    ('gen-a', 'scope-signals', 'poll', now(), now(), 'active'),
    ('gen-b', 'scope-empty-stages', 'poll', now(), now(), 'active'),
    ('gen-c', 'scope-repo-only', 'poll', now(), now(), 'active'),
    ('gen-d', 'scope-foreign', 'poll', now(), now(), 'active'),
    ('gen-e', 'scope-noname', 'poll', now(), now(), 'active'),
    ('gen-f', 'scope-jenkins-path', 'poll', now(), now(), 'active'),
    ('gen-g', 'scope-signal-free', 'poll', now(), now(), 'active');

INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES
    ('repo-a', 'scope-signals', 'gen-a', 'repository', 's', 'git', 'k', now(), now(), FALSE,
     '{"graph_id":"repo-a","name":"A"}'),
    ('file-a', 'scope-signals', 'gen-a', 'file', 's', 'git', 'k', now(), now(), FALSE,
     '{"repo_id":"repo-a","language":"dockerfile","relative_path":"Dockerfile","parsed_file_data":{"dockerfile_stages":[{"name":"runtime"}]}}'),
    ('repo-b', 'scope-empty-stages', 'gen-b', 'repository', 's', 'git', 'k', now(), now(), FALSE,
     '{"graph_id":"repo-b","name":"B"}'),
    ('file-b', 'scope-empty-stages', 'gen-b', 'file', 's', 'git', 'k', now(), now(), FALSE,
     '{"repo_id":"repo-b","language":"dockerfile","relative_path":"Dockerfile","parsed_file_data":{"dockerfile_stages":[]}}'),
    ('repo-c', 'scope-repo-only', 'gen-c', 'repository', 's', 'git', 'k', now(), now(), FALSE,
     '{"graph_id":"repo-c","name":"C"}'),
    ('repo-d', 'scope-foreign', 'gen-d', 'repository', 's', 'git', 'k', now(), now(), FALSE,
     '{"graph_id":"repo-d","name":"D"}'),
    ('file-d', 'scope-foreign', 'gen-d', 'file', 's', 'git', 'k', now(), now(), FALSE,
     '{"repo_id":"repo-other","language":"dockerfile","relative_path":"Dockerfile","parsed_file_data":{"dockerfile_stages":[{"name":"runtime"}]}}'),
    ('repo-e', 'scope-noname', 'gen-e', 'repository', 's', 'git', 'k', now(), now(), FALSE,
     '{"graph_id":"repo-e"}'),
    ('file-e', 'scope-noname', 'gen-e', 'file', 's', 'git', 'k', now(), now(), FALSE,
     '{"repo_id":"repo-e","language":"dockerfile","relative_path":"Dockerfile","parsed_file_data":{"dockerfile_stages":[{"name":"runtime"}]}}'),
    ('repo-f', 'scope-jenkins-path', 'gen-f', 'repository', 's', 'git', 'k', now(), now(), FALSE,
     '{"graph_id":"repo-f","name":"F"}'),
    ('file-f', 'scope-jenkins-path', 'gen-f', 'file', 's', 'git', 'k', now(), now(), FALSE,
     '{"repo_id":"repo-f","relative_path":"Jenkinsfile"}'),
    ('repo-g', 'scope-signal-free', 'gen-g', 'repository', 's', 'git', 'k', now(), now(), FALSE,
     '{"graph_id":"repo-g","name":"G"}'),
    ('file-g', 'scope-signal-free', 'gen-g', 'file', 's', 'git', 'k', now(), now(), FALSE,
     '{"repo_id":"repo-g","language":"yaml","relative_path":"config.yaml","parsed_file_data":{"unrelated_key":[1]}}');
`); err != nil {
		t.Fatalf("seed candidate-precondition fixture: %v", err)
	}

	store := NewFactStore(SQLDB{DB: db})

	matrix := []struct {
		name       string
		scopeID    string
		generation string
		want       bool
	}{
		{"dockerfile stages join identity", "scope-signals", "gen-a", true},
		{"empty stages hold no signal", "scope-empty-stages", "gen-b", false},
		{"repository alone holds none", "scope-repo-only", "gen-c", false},
		{"foreign repo_id joins nothing", "scope-foreign", "gen-d", false},
		{"nameless repository admits nothing", "scope-noname", "gen-e", false},
		{"bare Jenkinsfile path loads normally", "scope-jenkins-path", "gen-f", false},
		{"signal-free parsed payload holds none", "scope-signal-free", "gen-g", false},
	}
	for _, row := range matrix {
		t.Run(row.name, func(t *testing.T) {
			mayHold, err := store.ScopeMayHoldWorkloadCandidates(ctx, row.scopeID, row.generation)
			if err != nil {
				t.Fatalf("ScopeMayHoldWorkloadCandidates: %v", err)
			}
			if mayHold != row.want {
				t.Fatalf("mayHold = %v, want %v for %s/%s", mayHold, row.want, row.scopeID, row.generation)
			}
		})
	}
}
