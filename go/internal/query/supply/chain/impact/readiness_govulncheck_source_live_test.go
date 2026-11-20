// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #10029 disposable PostgreSQL proof that readiness
// reports the govulncheck source state from scanner_worker.warning facts and
// adds the source-state row when scanner results wrote none; reads
// ESHU_GOVULNCHECK_READINESS_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_GOVULNCHECK_READINESS_PROOF_DISPOSABLE ESHU_GOVULNCHECK_READINESS_PROOF_DSN ESHU_POSTGRES_DISPOSABLE ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract/packet"
	storagepostgres "github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

// govulncheckReadinessRepositoryID is the repository anchor the warning facts
// below name and the readiness query binds as $11.
const govulncheckReadinessRepositoryID = "repository:r_10029a01"

// seedGovulncheckWarningScope writes one active scope holding a single
// scanner_worker.warning fact with the given payload, returning the scope id.
func seedGovulncheckWarningScope(t *testing.T, ctx context.Context, db *sql.DB, scopeID, payload string) {
	t.Helper()
	for _, stmt := range []struct {
		query string
		args  []any
	}{
		{`INSERT INTO ingestion_scopes (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, payload)
VALUES ($1, 'repository', 'scanner_worker', $1, 'scanner_worker', $1, clock_timestamp(), clock_timestamp(), 'active', '{}'::jsonb)`, []any{scopeID}},
		{`INSERT INTO scope_generations (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
VALUES ($1, $1 || '-gen', 'sync', clock_timestamp(), clock_timestamp(), 'active')`, []any{scopeID}},
		{`UPDATE ingestion_scopes SET active_generation_id = $1 || '-gen' WHERE scope_id = $1`, []any{scopeID}},
		{`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1 || '-warning-1', $1, $1 || '-gen', 'scanner_worker.warning', $1 || '-warning-key-1', 'scanner_worker', $1 || '-warning-1', clock_timestamp(), clock_timestamp(), FALSE, $2::jsonb)`, []any{scopeID, payload}},
	} {
		if _, err := db.ExecContext(ctx, stmt.query, stmt.args...); err != nil {
			t.Fatalf("seed govulncheck warning scope: %v", err)
		}
	}
	for _, table := range []string{"fact_records", "ingestion_scopes", "scope_generations"} {
		if _, err := db.ExecContext(ctx, "ANALYZE "+table); err != nil {
			t.Fatalf("analyze %s: %v", table, err)
		}
	}
}

// TestReadinessReportsGovulncheckSourceStateLive is the #10029 accuracy proof
// for the govulncheck source-state row: an unfinished go_reachability scan's
// warning adds a partial govulncheck SourceState naming the failure class when
// scanner results wrote no stored row, while a finished scan, another
// analyzer's warning, or an already-stored govulncheck row adds nothing.
func TestReadinessReportsGovulncheckSourceStateLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_GOVULNCHECK_READINESS_PROOF_DSN"))
	var optIn string
	if dsn != "" {
		if os.Getenv("ESHU_GOVULNCHECK_READINESS_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_GOVULNCHECK_READINESS_PROOF_DSN is set without ESHU_GOVULNCHECK_READINESS_PROOF_DISPOSABLE=1")
		}
		optIn = os.Getenv("ESHU_GOVULNCHECK_READINESS_PROOF_DISPOSABLE")
	} else {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
		optIn = os.Getenv("ESHU_POSTGRES_DISPOSABLE")
	}
	if dsn == "" {
		t.Skip("set ESHU_GOVULNCHECK_READINESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the govulncheck readiness proof")
	}
	ctx, db := postgresproof.OpenDisposableBootstrapDatabase(t, dsn, optIn, 5*time.Minute, func(ctx context.Context, db *sql.DB) error {
		return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
	})
	seedGovulncheckWarningScope(t, ctx, db, "govulncheck-warn-scope", `{"analyzer": "go_reachability", "target_kind": "repository",
		"target_locator_hash": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"reason": "toolchain_unsupported", "warning_class": "scanner_worker_warning",
		"analysis_status": "not_scanned", "coverage_status": "unsupported",
		"repository_id": "`+govulncheckReadinessRepositoryID+`"}`)
	// A finished scan reports nothing unscanned, so it contributes no row.
	seedGovulncheckWarningScope(t, ctx, db, "govulncheck-done-scope", `{"analyzer": "go_reachability", "target_kind": "repository",
		"target_locator_hash": "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		"reason": "no_go_modules", "warning_class": "scanner_worker_warning",
		"analysis_status": "completed", "coverage_status": "scanned",
		"repository_id": "`+govulncheckReadinessRepositoryID+`"}`)
	// Another analyzer's warning speaks for that analyzer, not govulncheck.
	seedGovulncheckWarningScope(t, ctx, db, "govulncheck-other-scope", `{"analyzer": "image_ospkg", "target_kind": "image",
		"target_locator_hash": "sha256:2222222222222222222222222222222222222222222222222222222222222222",
		"reason": "analyzer_not_configured", "warning_class": "scanner_worker_warning",
		"analysis_status": "not_scanned", "coverage_status": "unsupported",
		"repository_id": "`+govulncheckReadinessRepositoryID+`"}`)

	store := NewPostgresReadinessStore(db)
	snapshot, err := store.ReadSupplyChainImpactReadiness(ctx, ReadinessQuery{RepositoryID: govulncheckReadinessRepositoryID})
	if err != nil {
		t.Fatalf("ReadSupplyChainImpactReadiness(): %v", err)
	}
	var govulncheck []packet.SourceState
	for _, state := range snapshot.SourceStates {
		if state.Source == "govulncheck" {
			govulncheck = append(govulncheck, state)
		}
	}
	if len(govulncheck) != 1 {
		t.Fatalf("govulncheck source states = %#v, want exactly the synthetic partial row", snapshot.SourceStates)
	}
	got := govulncheck[0]
	if got.Ecosystem != "gomod" {
		t.Fatalf("govulncheck ecosystem = %q, want gomod", got.Ecosystem)
	}
	if got.FreshnessState != "partial" {
		t.Fatalf("govulncheck freshness = %q, want partial", got.FreshnessState)
	}
	if got.LastErrorClass != "toolchain_unsupported" {
		t.Fatalf("govulncheck last_error_class = %q, want toolchain_unsupported", got.LastErrorClass)
	}
	if got.WarningCount != 1 {
		t.Fatalf("govulncheck warning_count = %d, want 1", got.WarningCount)
	}
	envelope := packet.BuildReadiness(packet.TargetScope{RepositoryID: govulncheckReadinessRepositoryID}, nil, false, snapshot)
	found := false
	for _, reason := range append(append([]string{}, envelope.MissingEvidence...), envelope.IncompleteReasons...) {
		if reason == "govulncheck:partial" {
			found = true
		}
	}
	if !found {
		t.Fatalf("readiness envelope missing govulncheck:partial: state=%q missing=%v incomplete=%v",
			envelope.State, envelope.MissingEvidence, envelope.IncompleteReasons)
	}
}

// seedGovulncheckModuleEvidence writes one active go_module_evidence fact
// into an already-seeded scope (the generation seedGovulncheckWarningScope
// created and activated), so the scope holds the module row a module-scoped
// warning joins through.
func seedGovulncheckModuleEvidence(t *testing.T, ctx context.Context, db *sql.DB, scopeID, payload string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1 || '-evidence-1', $1, $1 || '-gen', 'vulnerability.go_module_evidence', $1 || '-evidence-key-1', 'vulnerability_intelligence', $1 || '-evidence-1', clock_timestamp(), clock_timestamp(), FALSE, $2::jsonb)`, scopeID, payload); err != nil {
		t.Fatalf("seed govulncheck module evidence: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze fact_records: %v", err)
	}
}

// TestReadinessScopesGovulncheckWarningToItsModuleLive is the #11002 mirror
// proof for the govulncheck source-state row: a module-scoped warning
// (non-blank relative_path) contributes only when a go_module_evidence row
// in the same scope names that go.mod — the same rows the reducer joins
// through — while a warning for a go.mod with no evidence joins nothing and
// adds no row. Blank relative_path keeps repository scope (see
// TestReadinessReportsGovulncheckSourceStateLive, whose warning has none).
func TestReadinessScopesGovulncheckWarningToItsModuleLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_GOVULNCHECK_READINESS_PROOF_DSN"))
	var optIn string
	if dsn != "" {
		if os.Getenv("ESHU_GOVULNCHECK_READINESS_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_GOVULNCHECK_READINESS_PROOF_DSN is set without ESHU_GOVULNCHECK_READINESS_PROOF_DISPOSABLE=1")
		}
		optIn = os.Getenv("ESHU_GOVULNCHECK_READINESS_PROOF_DISPOSABLE")
	} else {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
		optIn = os.Getenv("ESHU_POSTGRES_DISPOSABLE")
	}
	if dsn == "" {
		t.Skip("set ESHU_GOVULNCHECK_READINESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the govulncheck readiness proof")
	}
	ctx, db := postgresproof.OpenDisposableBootstrapDatabase(t, dsn, optIn, 5*time.Minute, func(ctx context.Context, db *sql.DB) error {
		return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
	})
	// A module-scoped warning backed by module evidence for that go.mod.
	seedGovulncheckWarningScope(t, ctx, db, "govulncheck-modwarn-scope", `{"analyzer": "go_reachability", "target_kind": "repository",
		"target_locator_hash": "sha256:3333333333333333333333333333333333333333333333333333333333333333",
		"reason": "toolchain_unsupported", "warning_class": "scanner_worker_warning",
		"analysis_status": "not_scanned", "coverage_status": "unsupported",
		"repository_id": "`+govulncheckReadinessRepositoryID+`", "relative_path": "tools/go.mod"}`)
	seedGovulncheckModuleEvidence(t, ctx, db, "govulncheck-modwarn-scope", `{"repository_id": "`+govulncheckReadinessRepositoryID+`",
		"relative_path": "tools/go.mod", "module_path": "example.com/othermod",
		"required_version": "v1.2.3", "ecosystem": "go-module"}`)
	// A module-scoped warning for a go.mod with no evidence joins nothing.
	seedGovulncheckWarningScope(t, ctx, db, "govulncheck-orphanwarn-scope", `{"analyzer": "go_reachability", "target_kind": "repository",
		"target_locator_hash": "sha256:4444444444444444444444444444444444444444444444444444444444444444",
		"reason": "no_go_toolchain", "warning_class": "scanner_worker_warning",
		"analysis_status": "not_scanned", "coverage_status": "unsupported",
		"repository_id": "`+govulncheckReadinessRepositoryID+`", "relative_path": "missing/go.mod"}`)
	// A whitespace-padded path is non-blank, so it takes module scope, but
	// it byte-matches no evidence row and must join nothing (#11002).
	seedGovulncheckWarningScope(t, ctx, db, "govulncheck-paddedwarn-scope", `{"analyzer": "go_reachability", "target_kind": "repository",
		"target_locator_hash": "sha256:5555555555555555555555555555555555555555555555555555555555555555",
		"reason": "toolchain_unsupported", "warning_class": "scanner_worker_warning",
		"analysis_status": "not_scanned", "coverage_status": "unsupported",
		"repository_id": "`+govulncheckReadinessRepositoryID+`", "relative_path": " tools/go.mod "}`)
	seedGovulncheckModuleEvidence(t, ctx, db, "govulncheck-paddedwarn-scope", `{"repository_id": "`+govulncheckReadinessRepositoryID+`",
		"relative_path": "tools/go.mod", "module_path": "example.com/paddedmod",
		"required_version": "v1.2.3", "ecosystem": "go-module"}`)

	store := NewPostgresReadinessStore(db)
	snapshot, err := store.ReadSupplyChainImpactReadiness(ctx, ReadinessQuery{RepositoryID: govulncheckReadinessRepositoryID})
	if err != nil {
		t.Fatalf("ReadSupplyChainImpactReadiness(): %v", err)
	}
	var govulncheck []packet.SourceState
	for _, state := range snapshot.SourceStates {
		if state.Source == "govulncheck" {
			govulncheck = append(govulncheck, state)
		}
	}
	if len(govulncheck) != 1 {
		t.Fatalf("govulncheck source states = %#v, want exactly the evidenced module warning's row", snapshot.SourceStates)
	}
	got := govulncheck[0]
	if got.LastErrorClass != "toolchain_unsupported" {
		t.Fatalf("govulncheck last_error_class = %q, want toolchain_unsupported", got.LastErrorClass)
	}
	if got.WarningCount != 1 {
		t.Fatalf("govulncheck warning_count = %d, want 1", got.WarningCount)
	}
}

// TestReadinessKeepsStoredGovulncheckSourceStateLive pins the other half of
// the #10029 condition: when scanner results did write a govulncheck source
// state for the scope, the synthetic candidate stays out. (A stored row with
// no configured collector target stays invisible through the configured
// candidates' target gate, the pre-existing behavior for every source, so the
// observable here is the absence of the synthetic row: without the NOT EXISTS
// arm it would appear next to the warning.)
func TestReadinessKeepsStoredGovulncheckSourceStateLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_GOVULNCHECK_READINESS_PROOF_DSN"))
	var optIn string
	if dsn != "" {
		if os.Getenv("ESHU_GOVULNCHECK_READINESS_PROOF_DISPOSABLE") != "1" {
			t.Fatal("ESHU_GOVULNCHECK_READINESS_PROOF_DSN is set without ESHU_GOVULNCHECK_READINESS_PROOF_DISPOSABLE=1")
		}
		optIn = os.Getenv("ESHU_GOVULNCHECK_READINESS_PROOF_DISPOSABLE")
	} else {
		dsn = strings.TrimSpace(os.Getenv("ESHU_POSTGRES_DSN"))
		optIn = os.Getenv("ESHU_POSTGRES_DISPOSABLE")
	}
	if dsn == "" {
		t.Skip("set ESHU_GOVULNCHECK_READINESS_PROOF_DSN or ESHU_POSTGRES_DSN to run the govulncheck readiness proof")
	}
	ctx, db := postgresproof.OpenDisposableBootstrapDatabase(t, dsn, optIn, 5*time.Minute, func(ctx context.Context, db *sql.DB) error {
		return storagepostgres.ApplyBootstrap(ctx, storagepostgres.SQLDB{DB: db})
	})
	seedGovulncheckWarningScope(t, ctx, db, "govulncheck-warn-scope", `{"analyzer": "go_reachability", "target_kind": "repository",
		"target_locator_hash": "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"reason": "toolchain_unsupported", "warning_class": "scanner_worker_warning",
		"analysis_status": "not_scanned", "coverage_status": "unsupported",
		"repository_id": "`+govulncheckReadinessRepositoryID+`"}`)
	if _, err := db.ExecContext(ctx, `
INSERT INTO vulnerability_source_states (collector_instance_id, scope_id, source, ecosystem, last_attempt_at, last_error_class, freshness_state, terminal_status, result_count, warning_count, updated_at)
VALUES ('govulncheck-instance', 'govulncheck-warn-scope', 'govulncheck', 'gomod', clock_timestamp(), '', 'fresh', 'succeeded', 4, 0, clock_timestamp());
`); err != nil {
		t.Fatalf("seed stored govulncheck source state: %v", err)
	}

	store := NewPostgresReadinessStore(db)
	snapshot, err := store.ReadSupplyChainImpactReadiness(ctx, ReadinessQuery{RepositoryID: govulncheckReadinessRepositoryID})
	if err != nil {
		t.Fatalf("ReadSupplyChainImpactReadiness(): %v", err)
	}
	var govulncheck []packet.SourceState
	for _, state := range snapshot.SourceStates {
		if state.Source == "govulncheck" {
			govulncheck = append(govulncheck, state)
		}
	}
	if len(govulncheck) != 0 {
		t.Fatalf("govulncheck source states = %#v, want no synthetic row next to the stored one", snapshot.SourceStates)
	}
}
