// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package query

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

func writeMethodologyPostgresArtifact(t *testing.T, ctx context.Context, handle *sql.DB, proof methodologyPostgresCoverage) {
	t.Helper()
	manifest, err := queryplan.LoadManifestFile("../queryplan/testdata/handler-hot-cypher.yaml")
	if err != nil {
		t.Fatal(err)
	}
	identity := methodologyComparisonIdentity(t)
	base, candidate := identity.Base, identity.Candidate
	productionPaths := []string{"go/internal/query/cloud_resource_list_store.go"}
	for _, path := range methodologyPostgresMigrationPaths {
		productionPaths = append(productionPaths, "go/internal/storage/postgres/migrations/"+path)
	}
	methodologyVerifySameProduction(t, base, candidate, productionPaths)
	methodologyVerifySameProductionTree(t, base, candidate, "go/internal/storage/postgres/migrations", "*.sql")
	migrations := methodologyPostgresMigrations(t)
	schema, indexes := methodologyPostgresDefinitions(t, ctx, handle)
	config := methodologyPostgresConfig(t, ctx, handle)
	fixtureFiles := []string{"cloud_resource_list_store_live_test.go", "methodology_postgres_capture_test.go"}
	var fixture strings.Builder
	for _, path := range fixtureFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fixture.WriteString(path + "\n" + string(data) + "\n")
	}
	dataset := methodologyJSON(t, map[string]any{
		"version": "cloud-resource-arithmetic-v1", "seed": "integer sequence 1..20000",
		"distribution": map[string]any{
			"owners": 20001, "source_facts": 20000, "repository_scopes": 2,
			"provider_modulus": 4, "type_modulus": 20, "region_modulus": 8, "account_modulus": 16,
			"null_type_uid": 1, "tombstone_uid": 3, "retired_generation_uid": 5, "null_provider_uid": 9,
			"duplicate_fact_owner": "uid-duplicate backed by fact-000020",
		},
		"topology": "one disposable PostgreSQL instance and one serial client connection",
		"storage":  "session temporary tables; ANALYZE after seed; retained buffers; DISCARD PLANS before plan-cold samples",
	})
	workload := methodologyJSON(t, map[string]any{
		"variants": proof.Variants, "cases": proof.Cases,
		"normal_runs_per_side": 4, "profile_runs_per_side": 1, "pair_order": "AB,BA,AB,BA",
		"base_candidate": "production source identical; no query rewrite or speedup claim",
		"migrations":     "production index migrations executed unchanged as separate autocommit statements on temporary tables; PostgreSQL builds temporary indexes non-concurrently",
	})
	image := os.Getenv("ESHU_QUERY_METHODOLOGY_POSTGRES_IMAGE")
	if image == "" {
		t.Fatal("ESHU_QUERY_METHODOLOGY_POSTGRES_IMAGE must identify the actual disposable engine image")
	}
	var version string
	if err := handle.QueryRowContext(ctx, "SHOW server_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	version = strings.Fields(version)[0]
	artifact := queryplan.PilotEvidenceArtifact{
		Version: 1,
		Environment: queryplan.PilotRunEnvironment{
			Engine: "postgresql", EngineVersion: version, EngineImage: image,
			Config: config, ConfigSHA256: queryplan.PilotJSONSHA256(config),
			Dataset: dataset, DatasetSHA256: queryplan.PilotJSONSHA256(dataset),
			FixtureDefinition: fixture.String(), FixtureSHA256: queryplan.ProductionCypherSHA256(fixture.String()),
			Workload: workload, WorkloadSHA256: queryplan.PilotJSONSHA256(workload),
			HarnessSHA256: methodologySourceHash(t, []string{"methodology_postgres_live_test.go", "methodology_postgres_capture_test.go", "methodology_postgres_artifact_test.go", "methodology_identity_test.go"}),
			BinarySHA256:  methodologyBinaryHash(t), ColdReset: "cold_plan_warm_buffers",
		},
		Base:      methodologyBuild(t, base, schema, migrations, indexes),
		Candidate: methodologyBuild(t, candidate, schema, migrations, indexes),
	}
	for _, entry := range manifest.Entries {
		if entry.ID == "QP-CLOUD-RESOURCE-IDENTITY-PAGE" {
			artifact.Entries = []queryplan.PilotEvidenceEntry{{
				EntryID:        entry.ID,
				ContractSHA256: queryplan.PilotContractSHA256(*entry.Contract), SourceSHA256: entry.Source.SourceSHA256, Cases: proof.Evidence,
			}}
		}
	}
	path := os.Getenv("ESHU_QUERY_METHODOLOGY_POSTGRES_ARTIFACT")
	if path == "" {
		path = filepath.Join(t.TempDir(), "postgres-artifact.json")
	}
	methodologyWriteArtifact(t, path, artifact)
	if err := queryplan.ValidatePilotEvidenceForBackend(manifest, &artifact); err != nil {
		t.Fatalf("production PostgreSQL artifact: %v", err)
	}
	t.Logf("validated PostgreSQL artifact: %d variants/%d cases, base=%s candidate=%s", proof.Variants, proof.Cases, base, candidate)
}

func methodologyPostgresDefinitions(t *testing.T, ctx context.Context, handle *sql.DB) ([]string, []string) {
	t.Helper()
	// Catalog definitions include column types, nullability and constraints;
	// indexes retain pg_get_indexdef's complete expression and predicate.
	schema := methodologyPostgresStrings(t, ctx, handle, `
SELECT 'CREATE TEMP TABLE ' || quote_ident(c.relname) || ' (' ||
 string_agg(quote_ident(a.attname) || ' ' || format_type(a.atttypid,a.atttypmod) ||
 CASE WHEN a.attnotnull THEN ' NOT NULL' ELSE '' END, ', ' ORDER BY a.attnum) ||
 COALESCE((SELECT ', ' || string_agg(pg_get_constraintdef(k.oid), ', ' ORDER BY k.conname)
 FROM pg_constraint k WHERE k.conrelid=c.oid), '') || ')'
FROM pg_class c JOIN pg_attribute a ON a.attrelid=c.oid
WHERE c.relnamespace=pg_my_temp_schema() AND c.relkind='r' AND a.attnum>0 AND NOT a.attisdropped
GROUP BY c.oid,c.relname ORDER BY c.relname`)
	indexes := methodologyPostgresStrings(t, ctx, handle, `
SELECT pg_get_indexdef(i.indexrelid) FROM pg_index i JOIN pg_class c ON c.oid=i.indrelid
JOIN pg_class ix ON ix.oid=i.indexrelid WHERE c.relnamespace=pg_my_temp_schema()
ORDER BY ix.relname`)
	if len(schema) != 4 || len(indexes) < 4 {
		t.Fatalf("fixture catalog coverage: %d tables, %d indexes", len(schema), len(indexes))
	}
	return schema, indexes
}

func methodologyPostgresStrings(t *testing.T, ctx context.Context, handle *sql.DB, statement string) []string {
	t.Helper()
	rows, err := handle.QueryContext(ctx, statement)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	values := make([]string, 0)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return values
}

func methodologyPostgresConfig(t *testing.T, ctx context.Context, handle *sql.DB) []byte {
	t.Helper()
	settings := []string{"server_version", "shared_buffers", "work_mem", "max_parallel_workers_per_gather", "jit", "random_page_cost", "effective_cache_size", "plan_cache_mode"}
	config := make(map[string]string, len(settings))
	for _, name := range settings {
		var value string
		if err := handle.QueryRowContext(ctx, "SELECT current_setting($1)", name).Scan(&value); err != nil {
			t.Fatal(fmt.Errorf("capture setting %s: %w", name, err))
		}
		config[name] = value
	}
	return methodologyJSON(t, config)
}

var methodologyPostgresMigrationPaths = []string{
	"070_cloud_resource_owner_page_index.sql",
	"071_cloud_resource_owner_provider_page_index.sql",
	"072_cloud_resource_owner_region_page_index.sql",
	"073_cloud_resource_owner_account_page_index.sql",
}

func methodologyPostgresMigrations(t *testing.T) []string {
	t.Helper()
	definitions := make([]string, 0, len(methodologyPostgresMigrationPaths))
	for _, name := range methodologyPostgresMigrationPaths {
		data, err := os.ReadFile(filepath.Join("../storage/postgres/migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		definitions = append(definitions, string(data))
	}
	return definitions
}

func applyMethodologyPostgresMigrations(t *testing.T, ctx context.Context, handle *sql.DB) {
	t.Helper()
	names := []string{"graph_node_owner_cloud_resource_page_idx", "graph_node_owner_cloud_resource_provider_page_idx", "graph_node_owner_cloud_resource_region_page_idx", "graph_node_owner_cloud_resource_account_page_idx"}
	for _, name := range names {
		if _, err := handle.ExecContext(ctx, "DROP INDEX "+name); err != nil {
			t.Fatal(err)
		}
	}
	for _, definition := range methodologyPostgresMigrations(t) {
		// Each production statement runs separately in autocommit. PostgreSQL
		// builds temporary indexes non-concurrently; migration lock scheduling
		// across sessions remains outside this read pilot.
		if _, err := handle.ExecContext(ctx, definition); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := handle.ExecContext(ctx, "ANALYZE graph_node_owner"); err != nil {
		t.Fatal(err)
	}
}
