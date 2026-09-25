// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// MaxIndexKeyBytes is the largest total UTF-8 byte length Eshu writes into the
// string slots of one graph index key.
//
// Neo4j RANGE indexes (range-1.0) reject a key above a fixed size with
// "Property value is too large to index" (#7058). Measured on
// neo4j:2026-community (2026.08.1): a single string key is accepted up to 8164
// bytes and a (string, string, int) node-key constraint such as Function
// (name, path, line_number) up to 8151 string bytes in total. 8000 sits below
// both and leaves room for a third short string slot (K8sResource and
// CrossplaneClaim also key on kind). The same bound applies on every backend
// so graph truth does not depend on which backend is configured; NornicDB
// accepts larger values silently.
const MaxIndexKeyBytes = 8000

// IndexKey is one label-scoped property index or uniqueness constraint from
// the Go-owned graph schema.
type IndexKey struct {
	// Name is the DDL name, for example "function_unique".
	Name string
	// Label is the node label the index or constraint applies to.
	Label string
	// Properties are the indexed properties in DDL order.
	Properties []string
}

var (
	schemaIndexKeyPattern = regexp.MustCompile(
		`^CREATE (?:CONSTRAINT|INDEX) (\w+) IF NOT EXISTS FOR \(\s*\w+\s*:\s*(\w+)\s*\) (?:REQUIRE|ON) (.+?)(?: IS UNIQUE| IS NODE KEY)?$`,
	)
	schemaIndexPropertyPattern = regexp.MustCompile(`^\w+\.(\w+)$`)

	schemaIndexKeysOnce    sync.Once
	schemaIndexKeysByLabel map[string][][]string
)

// SchemaIndexKeys returns every node property index and uniqueness
// constraint the schema can create on any supported backend, deduplicated by
// DDL name. It returns an error naming each index statement whose shape it
// cannot read, so a new index shape cannot be silently left unguarded.
// Fulltext indexes are excluded: they tokenize values instead of storing one
// bounded key per node.
func SchemaIndexKeys() ([]IndexKey, error) {
	statements := make([]string, 0, len(schemaConstraints)+len(schemaPerformanceIndexes)+len(nornicDBMergeLookupIndexes)+2*len(uidConstraintLabels))
	// Raw constraints, not the dialect output: NornicDB drops composite
	// constraints but Neo4j still enforces them.
	statements = append(statements, schemaConstraints...)
	for _, backend := range []SchemaBackend{SchemaBackendNeo4j, SchemaBackendNornicDB} {
		stmts, err := SchemaStatementsForBackend(backend)
		if err != nil {
			return nil, err
		}
		statements = append(statements, stmts...)
	}

	seen := make(map[string]bool, len(statements))
	keys := make([]IndexKey, 0, len(statements))
	var errs []error
	for _, stmt := range statements {
		// DROP statements retire an older constraint (#7095); they create no
		// key to bound.
		if isFulltextSchemaStatement(stmt) || strings.HasPrefix(stmt, "DROP ") {
			continue
		}
		key, err := parseSchemaIndexKey(stmt)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if seen[key.Name] {
			continue
		}
		seen[key.Name] = true
		keys = append(keys, key)
	}
	return keys, errors.Join(errs...)
}

// SchemaIndexKeysByLabel returns the schema index keys grouped by label as
// property lists, computed once. Unreadable statements are omitted here; the
// SchemaIndexKeys error is what tests assert on.
func SchemaIndexKeysByLabel() map[string][][]string {
	schemaIndexKeysOnce.Do(func() {
		keys, _ := SchemaIndexKeys()
		schemaIndexKeysByLabel = make(map[string][][]string)
		for _, k := range keys {
			schemaIndexKeysByLabel[k.Label] = append(schemaIndexKeysByLabel[k.Label], k.Properties)
		}
	})
	return schemaIndexKeysByLabel
}

func isFulltextSchemaStatement(stmt string) bool {
	return strings.HasPrefix(stmt, "CALL ") || strings.Contains(stmt, "FULLTEXT")
}

// parseSchemaIndexKey reads one CREATE INDEX or CREATE CONSTRAINT statement on
// a node label.
func parseSchemaIndexKey(stmt string) (IndexKey, error) {
	m := schemaIndexKeyPattern.FindStringSubmatch(stmt)
	if m == nil {
		return IndexKey{}, fmt.Errorf("unrecognized schema index statement %q", stmt)
	}
	body := strings.TrimSpace(m[3])
	body = strings.TrimSuffix(strings.TrimPrefix(body, "("), ")")
	parts := strings.Split(body, ",")
	props := make([]string, 0, len(parts))
	for _, part := range parts {
		pm := schemaIndexPropertyPattern.FindStringSubmatch(strings.TrimSpace(part))
		if pm == nil {
			return IndexKey{}, fmt.Errorf("unrecognized property %q in schema index statement %q", part, stmt)
		}
		props = append(props, pm[1])
	}
	return IndexKey{Name: m[1], Label: m[2], Properties: props}, nil
}
