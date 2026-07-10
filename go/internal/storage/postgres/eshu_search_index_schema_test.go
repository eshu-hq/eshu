// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestBootstrapDefinitionsBoundEshuSearchIndexTermKeys(t *testing.T) {
	t.Parallel()

	marker := mustBootstrapDefinition(t, "eshu_search_index")
	for _, want := range []string{
		"term_key TEXT NOT NULL",
		"PRIMARY KEY (scope_id, generation_id, term_key, document_id)",
	} {
		if !strings.Contains(marker.SQL, want) {
			t.Fatalf("eshu_search_index SQL missing %q", want)
		}
	}
	if strings.Contains(marker.SQL, "PRIMARY KEY (scope_id, generation_id, term, document_id)") {
		t.Fatal("eshu_search_index terms still key raw term text")
	}
}

func TestBootstrapDefinitionsHashPartitionSearchIndexTerms(t *testing.T) {
	t.Parallel()

	marker := mustBootstrapDefinition(t, "eshu_search_index")
	for _, want := range []string{
		") PARTITION BY HASH (scope_id)",
		"CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p00",
		"CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p63",
		"FOR VALUES WITH (MODULUS 64, REMAINDER 0)",
		"FOR VALUES WITH (MODULUS 64, REMAINDER 63)",
		"PRIMARY KEY (scope_id, generation_id, term_key, document_id)",
	} {
		if !strings.Contains(marker.SQL, want) {
			t.Fatalf("eshu_search_index SQL missing partition fragment %q:\n%s", want, marker.SQL)
		}
	}
	for remainder := 0; remainder < 64; remainder++ {
		fragment := "FOR VALUES WITH (MODULUS 64, REMAINDER " + strconv.Itoa(remainder) + ")"
		if !strings.Contains(marker.SQL, fragment) {
			t.Fatalf("eshu_search_index SQL missing partition remainder %d:\n%s", remainder, marker.SQL)
		}
	}
}

func TestBootstrapDefinitionsSkipPartitionedSearchTermPKeyRebuild(t *testing.T) {
	t.Parallel()

	marker := mustBootstrapDefinition(t, "eshu_search_index")
	for _, want := range []string{
		"pkey_exists BOOLEAN",
		"IF NOT pkey_exists THEN",
		"ELSIF terms_relkind <> 'p' THEN",
	} {
		if !strings.Contains(marker.SQL, want) {
			t.Fatalf("eshu_search_index SQL missing partitioned PK rebuild guard %q:\n%s", want, marker.SQL)
		}
	}
}

func TestDataPlaneSearchIndexSchemaHashPartitionSearchIndexTerms(t *testing.T) {
	t.Parallel()

	schemaPath := filepath.Join("..", "..", "..", "..", "schema", "data-plane", "postgres", "003b_eshu_search_index.sql")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read data-plane search-index schema: %v", err)
	}
	sql := string(schema)
	for _, want := range []string{
		") PARTITION BY HASH (scope_id)",
		"CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p00",
		"CREATE TABLE IF NOT EXISTS eshu_search_index_terms_p63",
		"FOR VALUES WITH (MODULUS 64, REMAINDER 0)",
		"FOR VALUES WITH (MODULUS 64, REMAINDER 63)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("data-plane search-index schema missing partition fragment %q:\n%s", want, sql)
		}
	}
}

func TestDataPlaneSearchIndexSchemaSkipsPartitionedSearchTermPKeyRebuild(t *testing.T) {
	t.Parallel()

	schemaPath := filepath.Join("..", "..", "..", "..", "schema", "data-plane", "postgres", "003b_eshu_search_index.sql")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read data-plane search-index schema: %v", err)
	}
	sql := string(schema)
	for _, want := range []string{
		"pkey_exists BOOLEAN",
		"IF NOT pkey_exists THEN",
		"ELSIF terms_relkind <> 'p' THEN",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("data-plane search-index schema missing partitioned PK rebuild guard %q:\n%s", want, sql)
		}
	}
}

func TestBootstrapDefinitionsMigrateSearchTermsToHashPartitions(t *testing.T) {
	t.Parallel()

	migration := mustBootstrapDefinition(t, "partition_eshu_search_index_terms")
	for _, want := range []string{
		"eshu_search_index_terms_shadow",
		"PARTITION BY HASH (scope_id)",
		"LOCK TABLE eshu_search_index_terms",
		"EXCEPT",
		"ALTER TABLE eshu_search_index_terms RENAME TO eshu_search_index_terms_unpartitioned",
		"ALTER TABLE eshu_search_index_terms_shadow RENAME TO eshu_search_index_terms",
	} {
		if !strings.Contains(migration.SQL, want) {
			t.Fatalf("partition migration missing %q:\n%s", want, migration.SQL)
		}
	}
}

func TestBootstrapDefinitionsSearchTermCutoverAvoidsExclusiveReCopy(t *testing.T) {
	t.Parallel()

	migration := mustBootstrapDefinition(t, "partition_eshu_search_index_terms")
	if strings.Contains(migration.SQL, "ACCESS EXCLUSIVE") {
		t.Fatalf("partition migration should not hold ACCESS EXCLUSIVE during diff proof:\n%s", migration.SQL)
	}
	if strings.Contains(migration.SQL, "TRUNCATE TABLE eshu_search_index_terms_shadow") {
		t.Fatalf("partition migration should not full-recopy the shadow while locked:\n%s", migration.SQL)
	}
	const want = "LOCK TABLE eshu_search_index_terms IN SHARE MODE"
	if !strings.Contains(migration.SQL, want) {
		t.Fatalf("partition migration missing writer-blocking/read-allowing lock %q:\n%s", want, migration.SQL)
	}
}

func TestBootstrapDefinitionsDoNotRecreateHistoricalSearchTermDocumentIndex(t *testing.T) {
	t.Parallel()

	migration := mustBootstrapDefinition(t, "eshu_search_index_terms_doc_idx")
	if strings.Contains(migration.SQL, "CREATE INDEX CONCURRENTLY") {
		t.Fatalf("historical doc-index migration should not recreate the dropped term document index:\n%s", migration.SQL)
	}
}

func TestBootstrapDefinitionsAvoidRedundantSearchTermLookupIndex(t *testing.T) {
	t.Parallel()

	marker := mustBootstrapDefinition(t, "eshu_search_index")
	if !strings.Contains(marker.SQL, "PRIMARY KEY (scope_id, generation_id, term_key, document_id)") {
		t.Fatal("eshu_search_index terms primary key no longer covers term lookup prefix")
	}
	if strings.Contains(marker.SQL, "CREATE INDEX IF NOT EXISTS eshu_search_index_terms_lookup_idx") {
		t.Fatalf("eshu_search_index should not create redundant lookup index; "+
			"primary key prefix (scope_id, generation_id, term_key) covers BM25 term lookup:\n%s", marker.SQL)
	}
	if strings.Contains(marker.SQL, "DROP INDEX IF EXISTS eshu_search_index_terms_lookup_idx") {
		t.Fatalf("eshu_search_index should not drop lookup index non-concurrently; "+
			"038_drop_eshu_search_index_terms_lookup_idx owns the concurrent drop:\n%s", marker.SQL)
	}
}

func mustBootstrapDefinition(t *testing.T, name string) Definition {
	t.Helper()
	for _, def := range BootstrapDefinitions() {
		if def.Name == name {
			return def
		}
	}
	t.Fatalf("%s definition missing", name)
	return Definition{}
}

func TestDataPlaneSearchIndexSchemaAvoidsRedundantTermLookupIndex(t *testing.T) {
	t.Parallel()

	schemaPath := filepath.Join("..", "..", "..", "..", "schema", "data-plane", "postgres", "003b_eshu_search_index.sql")
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read data-plane search-index schema: %v", err)
	}
	sql := string(schema)
	if strings.Contains(sql, "CREATE INDEX IF NOT EXISTS eshu_search_index_terms_lookup_idx") {
		t.Fatalf("data-plane schema should not create redundant lookup index; "+
			"the primary key prefix covers BM25 term lookup:\n%s", sql)
	}
	if strings.Contains(sql, "DROP INDEX IF EXISTS eshu_search_index_terms_lookup_idx") {
		t.Fatalf("data-plane schema should not drop lookup index non-concurrently:\n%s", sql)
	}
	for _, want := range []string{
		"content_hash TEXT NOT NULL DEFAULT ''",
		"ADD COLUMN IF NOT EXISTS content_hash TEXT NOT NULL DEFAULT ''",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("data-plane search-index schema missing %q:\n%s", want, sql)
		}
	}
}

func TestBootstrapDefinitionsDropRedundantSearchTermLookupIndex(t *testing.T) {
	t.Parallel()

	var marker Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "drop_eshu_search_index_terms_lookup_idx" {
			marker = def
			break
		}
	}
	if marker.Name == "" {
		t.Fatal("drop_eshu_search_index_terms_lookup_idx definition missing")
	}
	const want = "DROP INDEX CONCURRENTLY IF EXISTS eshu_search_index_terms_lookup_idx"
	if !strings.Contains(marker.SQL, want) {
		t.Fatalf("drop lookup-index migration missing %q:\n%s", want, marker.SQL)
	}
}

// TestBootstrapDefinitionsDropSearchIndexTermsDocumentIndex asserts that the
// write-amplifying document-keyed term index is removed once the reducer stops
// issuing per-page document-keyed term refresh deletes.
func TestBootstrapDefinitionsDropSearchIndexTermsDocumentIndex(t *testing.T) {
	t.Parallel()

	var marker Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "drop_eshu_search_index_terms_doc_idx" {
			marker = def
			break
		}
	}
	if marker.Name == "" {
		t.Fatal("drop_eshu_search_index_terms_doc_idx definition missing")
	}
	const wantDrop = "DROP INDEX CONCURRENTLY IF EXISTS eshu_search_index_terms_doc_idx"
	if !strings.Contains(marker.SQL, wantDrop) {
		t.Fatalf("drop doc-index migration missing %q:\n%s", wantDrop, marker.SQL)
	}
	const wantConcurrently = "CONCURRENTLY"
	if !strings.Contains(marker.SQL, wantConcurrently) {
		t.Fatalf("drop doc-index migration missing %q:\n%s", wantConcurrently, marker.SQL)
	}
}
