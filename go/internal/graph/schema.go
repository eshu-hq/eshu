// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package graph schema.go provides Neo4j schema initialization.
//
// EnsureSchema creates all node constraints, performance indexes, and
// full-text indexes required by Eshu. The constraint
// and index definitions are the checked-in Go-owned schema contract for the
// rewritten platform. The flat DDL tables those routines iterate over live in
// schema_tables.go in this package.
package graph

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// SchemaBackend identifies the graph database dialect used for schema DDL.
type SchemaBackend string

const (
	// SchemaBackendNeo4j preserves Eshu's shared production schema contract.
	SchemaBackendNeo4j SchemaBackend = "neo4j"
	// SchemaBackendNornicDB applies the narrow compatibility dialect proven by
	// the opt-in NornicDB syntax gate.
	SchemaBackendNornicDB SchemaBackend = "nornicdb"
)

func nornicDBUIDLookupIndexes() []string {
	indexes := make([]string, 0, len(uidConstraintLabels))
	for _, label := range uidConstraintLabels {
		indexes = append(indexes, fmt.Sprintf(
			"CREATE INDEX nornicdb_%s_uid_lookup IF NOT EXISTS FOR (n:%s) ON (n.uid)",
			labelToSnake(label), label,
		))
	}
	return indexes
}

// EnsureSchema creates all constraints and indexes required by the platform
// context graph. Each statement is executed individually; failures are logged
// as warnings but do not abort the remaining statements. Full-text index
// creation automatically falls back to modern syntax when the procedure-based
// API is unavailable.
func EnsureSchema(ctx context.Context, executor CypherExecutor, logger *slog.Logger) error {
	return EnsureSchemaWithBackend(ctx, executor, logger, SchemaBackendNeo4j)
}

// EnsureSchemaWithBackend creates all constraints and indexes required by the
// selected graph backend. NornicDB uses only syntax translations proven by
// compatibility tests.
func EnsureSchemaWithBackend(ctx context.Context, executor CypherExecutor, logger *slog.Logger, backend SchemaBackend) error {
	return ensureSchemaWithBackend(ctx, executor, logger, backend, false)
}

// EnsureSchemaWithBackendStrict creates the selected graph backend schema and
// returns an error when any non-context DDL statement fails.
func EnsureSchemaWithBackendStrict(ctx context.Context, executor CypherExecutor, logger *slog.Logger, backend SchemaBackend) error {
	return ensureSchemaWithBackend(ctx, executor, logger, backend, true)
}

type schemaDialect struct {
	backend                   SchemaBackend
	constraint                func(string) string
	skipFulltextFallback      bool
	includeMergeLookupIndexes bool
}

func schemaDialectForBackend(backend SchemaBackend) (schemaDialect, error) {
	normalized, err := normalizeSchemaBackend(backend)
	if err != nil {
		return schemaDialect{}, err
	}
	switch normalized {
	case SchemaBackendNeo4j:
		return schemaDialect{backend: normalized, constraint: neo4jSchemaConstraint}, nil
	case SchemaBackendNornicDB:
		return schemaDialect{
			backend:                   normalized,
			constraint:                nornicDBSchemaConstraint,
			skipFulltextFallback:      true,
			includeMergeLookupIndexes: true,
		}, nil
	}
	return schemaDialect{}, fmt.Errorf("unsupported schema backend %q", backend)
}

func normalizeSchemaBackend(backend SchemaBackend) (SchemaBackend, error) {
	switch backend {
	case "", SchemaBackendNeo4j:
		return SchemaBackendNeo4j, nil
	case SchemaBackendNornicDB:
		return SchemaBackendNornicDB, nil
	default:
		return "", fmt.Errorf("unsupported schema backend %q", backend)
	}
}

func neo4jSchemaConstraint(cypher string) string {
	return cypher
}

func nornicDBSchemaConstraint(cypher string) string {
	if isCompositeUniqueConstraint(cypher) {
		// NornicDB's current parser accepts NODE KEY but rejects Neo4j's
		// composite IS UNIQUE form. Do not translate these constraints to
		// NODE KEY: node keys require every participating property to be
		// present, while several Eshu semantic labels are intentionally sparse.
		// Canonical writes use separate uid uniqueness constraints instead.
		return ""
	}
	return cypher
}

func isCompositeUniqueConstraint(cypher string) bool {
	return strings.Contains(cypher, " REQUIRE (") && strings.Contains(cypher, ") IS UNIQUE")
}

// executeSchemaStatement runs one DDL statement through the executor.
func executeSchemaStatement(ctx context.Context, executor CypherExecutor, cypher string) error {
	return executor.ExecuteCypher(ctx, CypherStatement{
		Cypher:     cypher,
		Parameters: map[string]any{},
	})
}
