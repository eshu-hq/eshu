// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiscoverStatementBuildersRecordsSitesOperationsAndTemplates(t *testing.T) {
	dir := t.TempDir()
	source := `package writer

import sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

func buildUpsert(label string) sourcecypher.Statement {
	return sourcecypher.Statement{
		Operation: sourcecypher.OperationCanonicalUpsert,
		Cypher:    "MATCH (n) RETURN n",
	}
}

func buildDynamic(label string) sourcecypher.Statement {
	return sourcecypher.Statement{
		Operation: opFor(label),
		Cypher:    "MATCH (n:" + label + ") RETURN n",
	}
}

func passthrough(stmt sourcecypher.Statement) sourcecypher.Statement {
	return stmt
}

func buildBatch() []sourcecypher.Statement {
	return []sourcecypher.Statement{
		{
			Operation: sourcecypher.OperationCanonicalRetract,
			Cypher:    "MATCH (n) DETACH DELETE n",
		},
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "writer.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "writer_test.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write test fixture: %v", err)
	}

	got, err := DiscoverStatementBuilders(dir)
	if err != nil {
		t.Fatalf("DiscoverStatementBuilders() error = %v", err)
	}
	// Digests pin the builder bodies; assert shape separately so the
	// comparison stays readable.
	for i := range got {
		for j := range got[i].Builders {
			digest := got[i].Builders[j].SourceDigest
			if len(digest) != 64 {
				t.Fatalf("Builder %s digest = %q, want 64 hex chars", got[i].Builders[j].Symbol, digest)
			}
			got[i].Builders[j].SourceDigest = ""
		}
	}
	want := []StatementBuilderCoverage{
		{
			File: "writer.go",
			Builders: []StatementBuilder{
				{Symbol: "buildBatch", Count: 1, Operation: "sourcecypher.OperationCanonicalRetract", Template: "MATCH (n) DETACH DELETE n"},
				{Symbol: "buildDynamic", Count: 1, Operation: "", Template: "", Dynamic: true},
				{Symbol: "buildUpsert", Count: 1, Operation: "sourcecypher.OperationCanonicalUpsert", Template: "MATCH (n) RETURN n"},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DiscoverStatementBuilders() = %#v, want %#v", got, want)
	}
}

func TestDiscoverStatementBuildersSkipsTestdataAndHelpers(t *testing.T) {
	dir := t.TempDir()
	source := `package writer

import sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"

func build() sourcecypher.Statement {
	return sourcecypher.Statement{Operation: sourcecypher.OperationCanonicalUpsert, Cypher: "MATCH (n) RETURN n"}
}
`
	if err := os.WriteFile(filepath.Join(dir, "writer.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write source fixture: %v", err)
	}
	testdataDir := filepath.Join(dir, "testdata")
	if err := os.MkdirAll(testdataDir, 0o700); err != nil {
		t.Fatalf("create testdata fixture directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(testdataDir, "fixture.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write testdata fixture: %v", err)
	}

	got, err := DiscoverStatementBuilders(dir)
	if err != nil {
		t.Fatalf("DiscoverStatementBuilders() error = %v", err)
	}
	if len(got) != 1 || got[0].File != "writer.go" {
		t.Fatalf("DiscoverStatementBuilders() = %#v, want only writer.go", got)
	}
}
