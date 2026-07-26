// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPackageRegistryCorrelationSQLProjectedFieldsAreSchemaDeclared closes the
// payload-usage manifest gate's blind spot for
// listPackageRegistryCorrelationsQuery's WHERE-clause filters (#5461, part of
// epic #5455). package_registry_correlations.go's row DECODE now goes through
// the typed factschema seam (factschema_decode_package_correlations.go),
// which IS visible to the #4573 payload-usage manifest gate. But the query's
// WHERE clause still reads package_id, repository_id, relationship_kind, and
// candidate_repository_ids straight off fact.payload with raw Postgres JSONB
// operators (`payload->>'field'` / `payload->'field'`) to filter rows BEFORE
// any row is fetched for decode — that SQL text is invisible to the manifest
// gate, the same blind spot
// TestDocumentationFindingSQLProjectedFieldsAreSchemaDeclared
// (documentation_sql_schema_lockstep_test.go) and
// TestPackageRegistrySQLProjectedFieldsAreSchemaDeclared
// (go/internal/storage/postgres) close for their own SQL constants. If a
// future contracts change dropped one of those fields from the three
// reducer_package_*_correlation schemas, the manifest gate would stay green
// while this filter predicate silently matched nothing.
//
// This test is the compile-adjacent lockstep that prevents it, reusing this
// package's documentationSQLReferencedFields/documentationSchemaDir helpers
// (documentation_sql_schema_lockstep_test.go): every field literal
// listPackageRegistryCorrelationsQuery reads via `payload->>'field'` or
// `payload->'field'` MUST appear as a declared property in at least one of
// the three reducer_package_*_correlation JSON Schemas.
//
// listPackageRegistryCorrelationsQuery cannot be added to the storage/postgres
// package's existing lockstep test instead: that SQL constant lives in this
// (query) package, and go/internal/storage/postgres must not import
// go/internal/query (this package already imports storage/postgres
// elsewhere, so the reverse import would cycle).
func TestPackageRegistryCorrelationSQLProjectedFieldsAreSchemaDeclared(t *testing.T) {
	t.Parallel()

	declared := packageCorrelationSchemaDeclaredProperties(t)

	// Fields listPackageRegistryCorrelationsQuery's WHERE clause reads
	// straight off fact.payload as a raw JSONB operator
	// (package_registry_correlations.go).
	sourceFields := []string{
		"package_id",
		"repository_id",
		"relationship_kind",
		"candidate_repository_ids",
	}

	referenced := documentationSQLReferencedFields(listPackageRegistryCorrelationsQuery)

	for _, field := range sourceFields {
		if !referenced[field] {
			t.Errorf("field %q is asserted here but no longer read by listPackageRegistryCorrelationsQuery; update this test", field)
			continue
		}
		if !declared[field] {
			t.Errorf("listPackageRegistryCorrelationsQuery reads payload field %q but none of the reducer_package_*_correlation schemas declare it as a property; a dropped/renamed field would silently break the filter", field)
		}
	}
}

// packageCorrelationSchemaDeclaredProperties reads the generated
// reducer_package_ownership_correlation.v1.schema.json,
// reducer_package_consumption_correlation.v1.schema.json, and
// reducer_package_publication_correlation.v1.schema.json JSON Schemas and
// returns the union of their declared property names.
func packageCorrelationSchemaDeclaredProperties(t *testing.T) map[string]bool {
	t.Helper()

	schemaDir := documentationSchemaDir(t)
	declared := map[string]bool{}
	for _, name := range []string{
		"reducer_package_ownership_correlation.v1.schema.json",
		"reducer_package_consumption_correlation.v1.schema.json",
		"reducer_package_publication_correlation.v1.schema.json",
	} {
		raw, err := os.ReadFile(filepath.Join(schemaDir, name))
		if err != nil {
			t.Fatalf("read schema %q: %v", name, err)
		}
		var doc struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("unmarshal schema %q: %v", name, err)
		}
		for prop := range doc.Properties {
			declared[prop] = true
		}
	}
	return declared
}
