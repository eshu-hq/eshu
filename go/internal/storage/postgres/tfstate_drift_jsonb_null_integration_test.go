package postgres

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

// Regression coverage for the production query bug surfaced by the chunk
// #166 compose proof matrix. Both the canonical-backend reader and the
// drift-evidence loader filter fact_records on
// `jsonb_typeof(...) = 'array' AND jsonb_array_length(...) > 0`. Postgres
// does NOT guarantee left-to-right short-circuit of AND predicates, so the
// length call can run on rows whose value is jsonb null (a scalar) and
// raise SQLSTATE 22023 "cannot get array length of a scalar". The fixed
// queries wrap the array_length call in a CASE that is only evaluated when
// the type guard matches; CASE branch evaluation IS guaranteed by the
// Postgres reference. These tests reproduce the failure mode by writing
// jsonb null at the load-bearing path next to a valid backend array and
// confirming the query returns the valid row without erroring.

const driftJSONBNullDSNEnv = "ESHU_POSTGRES_DSN"

func openDriftJSONBNullDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv(driftJSONBNullDSNEnv)
	if dsn == "" {
		t.Skipf("%s is not set; skipping jsonb null regression test", driftJSONBNullDSNEnv)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(context.Background()); err != nil {
		_ = db.Close()
		t.Fatalf("PingContext() error = %v, want nil", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// driftJSONBNullSeed installs the minimum schema plus one scope, one
// generation, one parser fact carrying terraform_backends as jsonb null
// (the row that triggered SQLSTATE 22023 under the original predicate
// order), and one parser fact carrying a real backend array. The test
// then exercises the SUT and asserts the bug-fixed query both succeeds
// and returns only the well-formed row.
func driftJSONBNullSeed(t *testing.T, db *sql.DB) (string, string, time.Time) {
	t.Helper()
	ctx := context.Background()

	// Assumes the DSN points to a database that has already had
	// eshu-bootstrap-data-plane (db-migrate in compose) applied. The
	// workflow_control_integration_test in the same package follows the
	// same convention; running these tests against an un-migrated DB
	// would fail at the first INSERT, which is the desired loud failure
	// for a misconfigured environment.

	scopeID := "repo_snapshot:jsonb-null-repro"
	generationID := "gen:jsonb-null-repro"
	observedAt := time.Date(2026, time.May, 11, 14, 30, 0, 0, time.UTC)

	if _, err := db.ExecContext(ctx, `
TRUNCATE fact_records, scope_generations, ingestion_scopes RESTART IDENTITY CASCADE
`); err != nil {
		t.Fatalf("TRUNCATE error = %v, want nil", err)
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes
    (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key,
     observed_at, ingested_at, status, active_generation_id)
VALUES ($1, 'repo_snapshot', 'git', 'jsonb-null-repro', 'git', 'jsonb-null-repro',
        $2, $2, 'active', $3)
ON CONFLICT (scope_id) DO NOTHING
`, scopeID, observedAt, generationID); err != nil {
		t.Fatalf("insert ingestion_scopes error = %v, want nil", err)
	}

	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations
    (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at)
VALUES ($1, $2, 'commit', $3, $3, 'active', $3)
ON CONFLICT (generation_id) DO NOTHING
`, generationID, scopeID, observedAt); err != nil {
		t.Fatalf("insert scope_generations error = %v, want nil", err)
	}

	return scopeID, generationID, observedAt
}

func driftJSONBNullInsertFact(t *testing.T, db *sql.DB, factID, scopeID, generationID string, observedAt time.Time, payloadJSON string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `
INSERT INTO fact_records
    (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
     source_fact_key, observed_at, ingested_at, payload)
VALUES ($1, $2, $3, 'file', $1, 'git', $1, $4, $4, $5::jsonb)
ON CONFLICT (fact_id) DO NOTHING
`, factID, scopeID, generationID, observedAt, payloadJSON); err != nil {
		t.Fatalf("insert fact_records (%s) error = %v, want nil", factID, err)
	}
}

// TestPostgresTerraformBackendQuerySurvivesNullTerraformBackendsPath proves
// the canonical backend reader does NOT raise SQLSTATE 22023 when the
// fact_records corpus contains rows whose
// parsed_file_data.terraform_backends value is jsonb null next to rows
// whose value is a valid array. The pre-fix query failed under exactly
// this input shape because Postgres reordered the array_length call ahead
// of the jsonb_typeof guard.
func TestPostgresTerraformBackendQuerySurvivesNullTerraformBackendsPath(t *testing.T) {
	db := openDriftJSONBNullDB(t)
	scopeID, generationID, observedAt := driftJSONBNullSeed(t, db)

	driftJSONBNullInsertFact(t, db, "fact:jsonb-null-tb", scopeID, generationID, observedAt, `{
        "repo_id":"jsonb-null-repro",
        "relative_path":"empty.tf",
        "parsed_file_data":{"terraform_backends": null}
    }`)
	driftJSONBNullInsertFact(t, db, "fact:valid-tb", scopeID, generationID, observedAt, `{
        "repo_id":"jsonb-null-repro",
        "relative_path":"main.tf",
        "parsed_file_data":{"terraform_backends":[{
            "backend_kind":"s3",
            "bucket":"jsonb-null-bucket",
            "bucket_is_literal":true,
            "key":"prod/terraform.tfstate",
            "key_is_literal":true,
            "region":"us-east-1",
            "region_is_literal":true
        }]}
    }`)

	hash := terraformstate.LocatorHash(terraformstate.StateKey{
		BackendKind: terraformstate.BackendS3,
		Locator:     "s3://jsonb-null-bucket/prod/terraform.tfstate",
	})

	query := PostgresTerraformBackendQuery{DB: SQLDB{DB: db}}
	rows, err := query.ListTerraformBackendsByLocator(context.Background(), "s3", hash)
	if err != nil {
		t.Fatalf("ListTerraformBackendsByLocator() error = %v, want nil (jsonb null row must not break the query)", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d; got=%#v", got, want, rows)
	}
	got := rows[0]
	want := tfstatebackend.TerraformBackendRow{
		RepoID:           "jsonb-null-repro",
		ScopeID:          scopeID,
		CommitID:         generationID,
		CommitObservedAt: observedAt.UTC(),
		BackendKind:      "s3",
		LocatorHash:      hash,
	}
	if got != want {
		t.Fatalf("rows[0] = %#v, want %#v", got, want)
	}
}

// TestPostgresDriftEvidenceLoaderSurvivesNullTerraformResourcesPath proves
// the drift evidence loader's config-side query does NOT raise SQLSTATE
// 22023 when fact_records includes rows with
// parsed_file_data.terraform_resources = jsonb null next to a row with a
// real array. Same predicate-reordering hazard as the canonical-backend
// reader; same CASE-wrapper fix.
func TestPostgresDriftEvidenceLoaderSurvivesNullTerraformResourcesPath(t *testing.T) {
	db := openDriftJSONBNullDB(t)
	scopeID, generationID, observedAt := driftJSONBNullSeed(t, db)

	driftJSONBNullInsertFact(t, db, "fact:jsonb-null-tr", scopeID, generationID, observedAt, `{
        "repo_id":"jsonb-null-repro",
        "relative_path":"empty.tf",
        "parsed_file_data":{"terraform_resources": null}
    }`)
	driftJSONBNullInsertFact(t, db, "fact:valid-tr", scopeID, generationID, observedAt, `{
        "repo_id":"jsonb-null-repro",
        "relative_path":"resources.tf",
        "parsed_file_data":{"terraform_resources":[{
            "resource_type":"aws_s3_bucket",
            "resource_name":"app"
        }]}
    }`)

	loader := PostgresDriftEvidenceLoader{DB: SQLDB{DB: db}}
	anchor := tfstatebackend.CommitAnchor{
		ScopeID:  scopeID,
		CommitID: generationID,
	}

	rows, err := loader.LoadDriftEvidence(context.Background(), "state_snapshot:s3:unused", anchor)
	if err != nil {
		t.Fatalf("LoadDriftEvidence() error = %v, want nil (jsonb null row must not break the loader)", err)
	}
	// The state-side input is absent (no terraform_state_snapshot fact), so
	// loadActiveStateSnapshot returns ok=false and LoadDriftEvidence returns
	// ([], nil) BEFORE the address-union runs. What matters here is that the
	// config-side query did not error first.
	if rows != nil {
		t.Fatalf("rows = %#v, want nil (state side absent so loader returns empty)", rows)
	}
}
