// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"reflect"
	"strings"
	"testing"
)

// TestSchemaIndexKeysCoverEveryIndexStatement is the guard against #7058
// coming back: every property index and uniqueness constraint the schema
// creates, on either backend, must appear in the key map the write-side
// oversized-key guard reads. A new DDL shape the parser cannot read fails
// here instead of leaving that index unguarded.
func TestSchemaIndexKeysCoverEveryIndexStatement(t *testing.T) {
	t.Parallel()

	keys, err := SchemaIndexKeys()
	if err != nil {
		t.Fatalf("SchemaIndexKeys() error = %v", err)
	}
	byName := make(map[string]IndexKey, len(keys))
	for _, k := range keys {
		byName[k.Name] = k
	}

	for _, backend := range []SchemaBackend{SchemaBackendNeo4j, SchemaBackendNornicDB} {
		stmts, err := SchemaStatementsForBackend(backend)
		if err != nil {
			t.Fatalf("SchemaStatementsForBackend(%s) error = %v", backend, err)
		}
		for _, stmt := range stmts {
			if isFulltextSchemaStatement(stmt) {
				continue
			}
			name := strings.Fields(stmt)[2]
			if _, ok := byName[name]; !ok {
				t.Errorf("schema statement %q has no entry in SchemaIndexKeys", stmt)
			}
		}
	}
	// Composite constraints are dropped from the NornicDB dialect but still
	// bind on Neo4j, so every raw constraint must be present too.
	for _, stmt := range schemaConstraints {
		if _, ok := byName[strings.Fields(stmt)[2]]; !ok {
			t.Errorf("constraint %q has no entry in SchemaIndexKeys", stmt)
		}
	}
}

func TestSchemaIndexKeysParsesCompositeAndSingleShapes(t *testing.T) {
	t.Parallel()

	byLabel := SchemaIndexKeysByLabel()
	cases := []struct {
		label string
		want  []string
	}{
		{label: "Function", want: []string{"name", "path", "line_number"}},
		{label: "K8sResource", want: []string{"name", "kind", "path", "line_number"}},
		{label: "TerraformModule", want: []string{"name", "path"}},
		{label: "TerraformStateResource", want: []string{"address"}},
		{label: "Struct", want: []string{"name", "path", "line_number"}},
		{label: "Module", want: []string{"name"}},
		{label: "File", want: []string{"path"}},
		{label: "Function", want: []string{"uid"}},
	}
	for _, tc := range cases {
		found := false
		for _, props := range byLabel[tc.label] {
			if reflect.DeepEqual(props, tc.want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("SchemaIndexKeysByLabel()[%q] = %v, want an entry %v", tc.label, byLabel[tc.label], tc.want)
		}
	}
}

func TestParseSchemaIndexKeyRejectsUnknownShape(t *testing.T) {
	t.Parallel()

	if _, err := parseSchemaIndexKey("CREATE INDEX odd IF NOT EXISTS FOR ()-[r:R]-() ON (r.x)"); err == nil {
		t.Fatal("parseSchemaIndexKey(relationship index) error = nil, want an error so the shape is not silently unguarded")
	}
}
