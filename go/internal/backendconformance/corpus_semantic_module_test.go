// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import (
	"strings"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// TestSemanticModuleCasesAreInTheCorpora keeps the cases from silently falling
// out of what the live test runs: the reads in DefaultReadCorpus with exact
// rows, the writes in WriteCorpusFor for both backends.
func TestSemanticModuleCasesAreInTheCorpora(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		semanticModuleAbsentFileReadCaseName,
		semanticModulePresentFileReadCaseName,
		semanticModuleImportReadCaseName,
	} {
		c, ok := readCaseByName(name)
		if !ok {
			t.Errorf("read case %q is absent from DefaultReadCorpus", name)
			continue
		}
		if c.WantRows == nil {
			t.Errorf("read case %q has no WantRows; a row count cannot see a uid-bearing vs uid-NULL module", name)
		}
	}
	for _, backend := range []BackendID{BackendNornicDB, BackendNeo4j} {
		corpus, err := WriteCorpusFor(backend)
		if err != nil {
			t.Fatalf("WriteCorpusFor(%s) error = %v", backend, err)
		}
		names := map[string]bool{}
		for _, c := range corpus {
			names[c.Name] = true
			if err := validateWriteCase(c); err != nil {
				t.Errorf("WriteCorpusFor(%s) case %q invalid: %v", backend, c.Name, err)
			}
		}
		for _, name := range []string{
			semanticModuleAbsentFileWriteCaseName,
			semanticModuleFileSeedCaseName,
			semanticModulePresentFileWriteCaseName,
			semanticModuleImportWriteCaseName,
		} {
			if !names[name] {
				t.Errorf("WriteCorpusFor(%s) lacks write case %q", backend, name)
			}
		}
	}
}

// TestSemanticModuleAbsentFileReadWantsNoRows pins the decided outcome: a row
// whose File is absent creates no Module. An empty non-nil WantRows is what
// makes RunReadCorpus require zero rows; nil would disable the check.
// Neither backend may override the correct outcome.
func TestSemanticModuleAbsentFileReadWantsNoRows(t *testing.T) {
	t.Parallel()

	c, ok := readCaseByName(semanticModuleAbsentFileReadCaseName)
	if !ok {
		t.Fatalf("read case %q missing", semanticModuleAbsentFileReadCaseName)
	}
	if c.WantRows == nil || len(c.WantRows) != 0 {
		t.Fatalf("WantRows = %#v, want empty non-nil", c.WantRows)
	}
	if _, ok := c.Overrides[BackendNeo4j]; ok {
		t.Fatalf("absent-file case overrides neo4j; only the backend that diverges may be pinned")
	}
	if _, ok := c.Overrides[BackendNornicDB]; ok {
		t.Fatalf("absent-file NornicDB override = %#v, want shared correct rows", c.Overrides)
	}
	imp, ok := readCaseByName(semanticModuleImportReadCaseName)
	if !ok {
		t.Fatalf("read case %q missing", semanticModuleImportReadCaseName)
	}
	if len(imp.WantRows) != 1 || imp.WantRows[0]["uid"] != nil {
		t.Fatalf("import WantRows = %#v, want exactly one uid-NULL row", imp.WantRows)
	}
	if _, ok := imp.Overrides[BackendNeo4j]; ok {
		t.Fatalf("import case overrides neo4j; only the backend that diverges may be pinned")
	}
	if _, ok := imp.Overrides[BackendNornicDB]; ok {
		t.Fatalf("import NornicDB override = %#v, want shared correct rows", imp.Overrides)
	}
}

// TestSemanticModuleCasesMatchFileBeforeMergeOnBothBackends keeps the production
// writer's Module upsert gated by File existence on each backend.
func TestSemanticModuleCasesMatchFileBeforeMergeOnBothBackends(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		backend BackendID
		prefix  string
	}{
		{BackendNeo4j, "UNWIND $rows AS row\nMATCH (f:File {path: row.file_path})\nMERGE (n:Module {uid: row.entity_id})"},
		{BackendNornicDB, "UNWIND $rows AS row\nMATCH (f:File {path: row.file_path})\nMERGE (n:Module {uid: row.entity_id})"},
	} {
		upsert := semanticModuleUpsertStatement(t, tc.backend, semanticModuleAbsentFileWriteCaseName)
		if !strings.HasPrefix(upsert.Cypher, tc.prefix) {
			t.Errorf("%s module upsert = %q, want prefix %q", tc.backend, upsert.Cypher, tc.prefix)
		}
	}
}

// TestSemanticModuleStatementsAreSanitized checks no `_eshu_*` diagnostic key
// reaches the live driver, matching SanitizeStatementParameters in production.
func TestSemanticModuleStatementsAreSanitized(t *testing.T) {
	t.Parallel()

	for _, backend := range []BackendID{BackendNornicDB, BackendNeo4j} {
		corpus, err := WriteCorpusFor(backend)
		if err != nil {
			t.Fatalf("WriteCorpusFor(%s) error = %v", backend, err)
		}
		for _, c := range corpus {
			for _, stmt := range c.Statements {
				for key := range stmt.Parameters {
					if strings.HasPrefix(key, "_") {
						t.Errorf("%s case %q carries diagnostic parameter %q", backend, c.Name, key)
					}
				}
			}
		}
	}
}

func TestSemanticEntityWriterForRejectsUnknownBackend(t *testing.T) {
	t.Parallel()

	if _, err := WriteCorpusFor(BackendID("memgraph")); err == nil {
		t.Fatal("WriteCorpusFor(memgraph) error = nil, want unknown-backend error")
	}
}

// semanticModuleUpsertStatement returns the Module upsert statement in the
// named write case of backend's corpus.
func semanticModuleUpsertStatement(t *testing.T, backend BackendID, caseName string) sourcecypher.Statement {
	t.Helper()
	corpus, err := WriteCorpusFor(backend)
	if err != nil {
		t.Fatalf("WriteCorpusFor(%s) error = %v", backend, err)
	}
	for _, c := range corpus {
		if c.Name != caseName {
			continue
		}
		for _, stmt := range c.Statements {
			if strings.Contains(stmt.Cypher, "(n:Module {uid: row.entity_id})") {
				return stmt
			}
		}
	}
	t.Fatalf("%s case %q has no Module upsert statement", backend, caseName)
	return sourcecypher.Statement{}
}
