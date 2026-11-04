// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestScopeMayHoldWorkloadCandidatesLive is the #9697 live proof for the
// workload bulk-load pre-gate probe, mirroring
// TestListActiveContainerImageCIFactsOwnerScopedQueryLive's isolated-schema
// pattern: against a real Postgres planner the probe reports true only for
// the scope generation whose repository identity joins to a file fact holding
// a signal VALUE extraction reads (#9986: null, empty, and blank values do
// not count), and false for every shape with no such pair (repo-only,
// signal-free or null-only parsed payload, foreign repo_id, nameless
// repository). TestScopeMayHoldWorkloadCandidatesDifferentialLive proves the
// same predicate against ExtractWorkloadCandidates over a generated corpus.
func TestScopeMayHoldWorkloadCandidatesLive(t *testing.T) {
	const schema = "eshu_9697_candidate_precondition_live"
	ctx, db := openCandidateProbeSchema(t, schema, 60*time.Second)

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES
    ('scope-signals', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-a'),
    ('scope-empty-stages', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-b'),
    ('scope-repo-only', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-c'),
    ('scope-foreign', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-d'),
    ('scope-noname', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-e'),
    ('scope-jenkins-path', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-f'),
    ('scope-signal-free', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-g'),
    ('scope-two-gens', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-i2'),
    ('scope-nograph', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-j'),
    ('scope-tombstone', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-k'),
    ('scope-qa-null', 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', 'gen-l');

INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES
    ('gen-a', 'scope-signals', 'poll', now(), now(), 'active'),
    ('gen-b', 'scope-empty-stages', 'poll', now(), now(), 'active'),
    ('gen-c', 'scope-repo-only', 'poll', now(), now(), 'active'),
    ('gen-d', 'scope-foreign', 'poll', now(), now(), 'active'),
    ('gen-e', 'scope-noname', 'poll', now(), now(), 'active'),
    ('gen-f', 'scope-jenkins-path', 'poll', now(), now(), 'active'),
    ('gen-g', 'scope-signal-free', 'poll', now(), now(), 'active'),
    ('gen-i1', 'scope-two-gens', 'poll', now(), now(), 'superseded'),
    ('gen-i2', 'scope-two-gens', 'poll', now(), now(), 'active'),
    ('gen-j', 'scope-nograph', 'poll', now(), now(), 'active'),
    ('gen-k', 'scope-tombstone', 'poll', now(), now(), 'active'),
    ('gen-l', 'scope-qa-null', 'poll', now(), now(), 'active');

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
     '{"repo_id":"repo-f","relative_path":"Jenkinsfile","parsed_file_data":{}}'),
    ('repo-g', 'scope-signal-free', 'gen-g', 'repository', 's', 'git', 'k', now(), now(), FALSE,
     '{"graph_id":"repo-g","name":"G"}'),
    ('file-g', 'scope-signal-free', 'gen-g', 'file', 's', 'git', 'k', now(), now(), FALSE,
     '{"repo_id":"repo-g","language":"yaml","relative_path":"config.yaml","parsed_file_data":{"unrelated_key":[1]}}'),
    -- Older generation carries the signal; the newer one has only the repository.
    ('repo-i1', 'scope-two-gens', 'gen-i1', 'repository', 's', 'git', 'k', now(), now(), FALSE,
     '{"graph_id":"repo-i","name":"I"}'),
    ('file-i1', 'scope-two-gens', 'gen-i1', 'file', 's', 'git', 'k', now(), now(), FALSE,
     '{"repo_id":"repo-i","language":"dockerfile","relative_path":"Dockerfile","parsed_file_data":{"dockerfile_stages":[{"name":"runtime"}]}}'),
    ('repo-i2', 'scope-two-gens', 'gen-i2', 'repository', 's', 'git', 'k', now(), now(), FALSE,
     '{"graph_id":"repo-i","name":"I"}'),
    -- Empty graph_id admits nothing even when the file repo_id is also empty.
    ('repo-j', 'scope-nograph', 'gen-j', 'repository', 's', 'git', 'k', now(), now(), FALSE,
     '{"graph_id":"","name":"J"}'),
    ('file-j', 'scope-nograph', 'gen-j', 'file', 's', 'git', 'k', now(), now(), FALSE,
     '{"repo_id":"","language":"dockerfile","relative_path":"Dockerfile","parsed_file_data":{"dockerfile_stages":[{"name":"runtime"}]}}'),
    -- Tombstones are intentionally NOT filtered: the scope-local kind loads
    -- hand tombstoned envelopes to extraction, so the probe sees the same input.
    ('repo-k', 'scope-tombstone', 'gen-k', 'repository', 's', 'git', 'k', now(), now(), TRUE,
     '{"graph_id":"repo-k","name":"K"}'),
    ('file-k', 'scope-tombstone', 'gen-k', 'file', 's', 'git', 'k', now(), now(), TRUE,
     '{"repo_id":"repo-k","language":"dockerfile","relative_path":"Dockerfile","parsed_file_data":{"dockerfile_stages":[{"name":"runtime"}]}}'),
    -- The shape QA stores for a file with no deployment evidence (#9986): every
    -- signal key present as JSON null or an empty list.
    ('repo-l', 'scope-qa-null', 'gen-l', 'repository', 's', 'git', 'k', now(), now(), FALSE,
     '{"graph_id":"repo-l","name":"L"}'),
    ('file-l', 'scope-qa-null', 'gen-l', 'file', 's', 'git', 'k', now(), now(), FALSE,
     '{"repo_id":"repo-l","language":"yaml","relative_path":"a.yaml","parsed_file_data":{"k8s_resources":null,"argocd_applications":null,"argocd_applicationsets":null,"chart_type":null,"artifact_type":"","cloudformation_resources":null,"cloudformation_parameters":null,"cloudformation_outputs":null,"cloudformation_cross_stack_imports":null,"cloudformation_cross_stack_exports":null,"dockerfile_stages":[],"pipeline_calls":[],"docker_compose_services":[]}}');
`); err != nil {
		t.Fatalf("seed candidate-precondition fixture: %v", err)
	}

	// One scope generation per signal key, derived from the shipped list, so a
	// key the bound array drops (or mangles) turns exactly one subtest red.
	for i, key := range factload.WorkloadCandidateSignalKeys {
		scopeID := fmt.Sprintf("scope-key-%d", i)
		generationID := fmt.Sprintf("gen-key-%d", i)
		repoID := fmt.Sprintf("repo-key-%d", i)
		for _, stmt := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
VALUES ($1, 'git', 'git', 'k', 'git', 'p0', now(), now(), 'active', $2)`, []any{scopeID, generationID}},
			{`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $2, 'poll', now(), now(), 'active')`, []any{generationID, scopeID}},
			{
				`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES
    ('repo-' || $2::text, $1, $2, 'repository', 's', 'git', 'k', now(), now(), FALSE, jsonb_build_object('graph_id', $3::text, 'name', 'N')),
    ('file-' || $2::text, $1, $2, 'file', 's', 'git', 'k', now(), now(), FALSE,
     jsonb_build_object('repo_id', $3::text, 'relative_path', 'x', 'parsed_file_data', jsonb_build_object($4::text, $5::jsonb)))`,
				[]any{scopeID, generationID, repoID, key, probeSignalValueJSON(key)},
			},
		} {
			if _, err := db.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
				t.Fatalf("seed signal key %q: %v", key, err)
			}
		}
	}

	store := NewFactStore(SQLDB{DB: db})

	matrix := []struct {
		name       string
		scopeID    string
		generation string
		want       bool
	}{
		{"dockerfile stages join identity", "scope-signals", "gen-a", true},
		// The probe tests values the way extraction reads them: an empty
		// bucket is not a signal. It stays a superset of extraction (language
		// and path gates are not repeated), so the post-load gates decide.
		{"empty signal bucket holds nothing", "scope-empty-stages", "gen-b", false},
		{"every signal key null or empty holds nothing", "scope-qa-null", "gen-l", false},
		{"repository alone holds none", "scope-repo-only", "gen-c", false},
		{"foreign repo_id joins nothing", "scope-foreign", "gen-d", false},
		{"nameless repository admits nothing", "scope-noname", "gen-e", false},
		{"Jenkinsfile path with an empty parsed payload", "scope-jenkins-path", "gen-f", true},
		{"signal-free parsed payload holds none", "scope-signal-free", "gen-g", false},
		{"newer generation without a signal file holds none", "scope-two-gens", "gen-i2", false},
		{"older generation signal is probed on its own generation", "scope-two-gens", "gen-i1", true},
		{"generation of another scope joins nothing", "scope-signals", "gen-d", false},
		{"unknown generation holds none", "scope-signals", "gen-missing", false},
		{"empty graph_id admits nothing", "scope-nograph", "gen-j", false},
		{"tombstoned rows are intentionally not filtered", "scope-tombstone", "gen-k", true},
	}
	for i, key := range factload.WorkloadCandidateSignalKeys {
		matrix = append(matrix, struct {
			name       string
			scopeID    string
			generation string
			want       bool
		}{
			fmt.Sprintf("signal key %s", key),
			fmt.Sprintf("scope-key-%d", i),
			fmt.Sprintf("gen-key-%d", i),
			true,
		})
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

// TestScopeMayHoldWorkloadCandidatesDifferentialLive is the #9986 accuracy
// proof for the value-aware probe: for every generated scope generation it
// runs the shipped probe statement and reducer.ExtractWorkloadCandidates on
// the identical payload JSON, and fails when the probe answers false although
// extraction yields a candidate. It also reports how often the probe answers
// true although extraction yields none (a non-counting deferral, never a
// wrong graph) next to the key-existence baseline.
func TestScopeMayHoldWorkloadCandidatesDifferentialLive(t *testing.T) {
	ctx, db := openCandidateProbeSchema(t, "eshu_9986_candidate_differential_live", 240*time.Second)
	cases := probeDiffCases()
	seedProbeDiffCases(ctx, t, db, cases)
	store := NewFactStore(SQLDB{DB: db})
	keys := array.StringArray(factload.WorkloadCandidateSignalKeys)

	var falseNegatives, falsePositives, extractTrue, probeTrue, baselineTrue, baselineFalsePositive int
	var missed, loose, falsePositiveSamples []string
	for i, c := range cases {
		scopeID, generationID := fmt.Sprintf("scope-d%d", i), fmt.Sprintf("gen-d%d", i)
		mayHold, err := store.ScopeMayHoldWorkloadCandidates(ctx, scopeID, generationID)
		if err != nil {
			t.Fatalf("probe %q: %v", c.name, err)
		}
		var baseline bool
		if err := db.QueryRowContext(ctx, keyExistenceBaselineSQL, scopeID, generationID, keys).Scan(&baseline); err != nil {
			t.Fatalf("baseline probe %q: %v", c.name, err)
		}
		hasCandidate := len(extractProbeDiffCandidates(t, c)) > 0
		if c.mustBeFalse && mayHold && len(loose) < 20 {
			loose = append(loose, c.name)
		}
		switch {
		case hasCandidate && !mayHold:
			falseNegatives++
			if len(missed) < 20 {
				missed = append(missed, c.name)
			}
		case !hasCandidate && mayHold:
			falsePositives++
			if len(falsePositiveSamples) < 60 {
				falsePositiveSamples = append(falsePositiveSamples, c.name)
			}
		}
		if hasCandidate {
			extractTrue++
		}
		if mayHold {
			probeTrue++
		}
		if baseline {
			baselineTrue++
			if !hasCandidate {
				baselineFalsePositive++
			}
		}
	}
	t.Logf("synthetic corpus: cases=%d candidate=%d probe_true=%d false_positive=%d false_negative=%d "+
		"key_existence_true=%d key_existence_false_positive=%d",
		len(cases), extractTrue, probeTrue, falsePositives, falseNegatives, baselineTrue, baselineFalsePositive)
	t.Logf("false-positive samples (language and path gates the probe does not repeat):\n%s",
		strings.Join(falsePositiveSamples, "\n"))
	if len(loose) != 0 {
		t.Fatalf("probe answered true for null/empty/blank-only generations, e.g.:\n%s", strings.Join(loose, "\n"))
	}
	if falseNegatives != 0 {
		sort.Strings(missed)
		t.Fatalf("probe answered false for %d generations that extraction turns into candidates, e.g.:\n%s",
			falseNegatives, strings.Join(missed, "\n"))
	}
}

// openCandidateProbeSchema creates an isolated schema holding the three
// tables the candidate probe reads (ingestion_scopes, scope_generations,
// fact_records), returns a single-connection handle bound to it, and drops
// the schema on cleanup. It skips the test when ESHU_POSTGRES_TEST_DSN is
// unset. The returned context carries the given timeout and is cancelled on
// cleanup.
func openCandidateProbeSchema(t *testing.T, schema string, timeout time.Duration) (context.Context, *sql.DB) {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live candidate-precondition proofs")
	}
	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	adminDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = adminDB.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	t.Cleanup(cancel)
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
`); err != nil {
		t.Fatalf("create candidate-precondition tables: %v", err)
	}
	return ctx, db
}

// probeSignalValueJSON returns a value extraction reads as evidence for one
// signal key, chosen by value class so a per-key live row proves the probe
// reads that key the way extraction does.
func probeSignalValueJSON(key string) string {
	for _, tokenKey := range factload.WorkloadCandidateTokenKeys {
		if key == tokenKey {
			return `"docker_compose"`
		}
	}
	for _, presenceKey := range factload.WorkloadCandidatePresenceKeys {
		if key == presenceKey {
			return `"stable"`
		}
	}
	return `[1]`
}
