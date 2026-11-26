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

// nornicDBUIDLookupIndexDrops guards the 1.5.x upgrade (#11535): stores
// bootstrapped by the pre-#11535 dialect still carry a plain
// nornicdb_<t>_uid_lookup index on every uid-bearing label, and NornicDB
// 1.5.x rejects the uniqueness constraint on the same (label, uid) with
// IndexAlreadyExists while that index exists. The drops run before the uid
// constraints (IF EXISTS keeps them idempotent on a fresh store), and the
// constraint's own 1.5.x backing index — named <t>_uid_unique, the same as
// the constraint — is the only uid index afterwards.
//
// This statement list must only ever execute against a 1.5.x backend: on
// pre-1.5 NornicDB the constraint creates no backing index, so dropping the
// lookup indexes there would leave uid reads unindexed. The rollout keeps
// the backend pin and this dialect in lockstep.
func nornicDBUIDLookupIndexDrops() []string {
	drops := make([]string, 0, len(uidConstraintLabels))
	for _, label := range uidConstraintLabels {
		drops = append(drops, fmt.Sprintf(
			"DROP INDEX nornicdb_%s_uid_lookup IF EXISTS",
			labelToSnake(label),
		))
	}
	return drops
}

// EnsureSchema creates all constraints and indexes required by the platform
// context graph. Each statement is executed individually; failures are logged
// as warnings but do not abort the remaining statements. Each backend runs
// its single full-text form: modern CREATE FULLTEXT INDEX on Neo4j, the
// procedure form on NornicDB (#7675).
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
	includeMergeLookupIndexes bool
	// includeNeo4jUIDLookupIndexes adds neo4jUIDLookupIndexes (#7057).
	includeNeo4jUIDLookupIndexes bool
	// includeInfraSearchKeyIndex adds the #7245 infra search key FULLTEXT
	// index (neo4jInfraSearchKeyIndexes).
	includeInfraSearchKeyIndex bool
	// includeRetirementLookupIndexes adds the #8659 retirement RANGE
	// indexes (neo4jRetirementLookupIndexes). Neo4j only: NornicDB keeps
	// its own same-shape lookup indexes, and a second property index on
	// the same label and property would re-backfill every existing
	// NornicDB store (nornicdb-pitfalls.md).
	includeRetirementLookupIndexes bool
	// retiredConstraints names the uniqueness constraints narrower than the
	// canonical uid identity that this dialect drops (DROP CONSTRAINT ... IF
	// EXISTS) before any other statement (#7095, #7097).
	retiredConstraints []string
	// dropUIDLookupIndexes emits the DROP INDEX guards for the retired
	// nornicdb_<t>_uid_lookup indexes (#11535) immediately before the uid
	// constraints. One flag gates the statement list, the executor, and the
	// statement count together so the three cannot drift.
	dropUIDLookupIndexes bool
	// retiredConstraintPathIndexes are the non-unique path indexes that keep the
	// reads the retired constraints served. Empty when no read needs one.
	retiredConstraintPathIndexes []string
}

func schemaDialectForBackend(backend SchemaBackend) (schemaDialect, error) {
	normalized, err := normalizeSchemaBackend(backend)
	if err != nil {
		return schemaDialect{}, err
	}
	switch normalized {
	case SchemaBackendNeo4j:
		return schemaDialect{
			backend:                        normalized,
			constraint:                     neo4jSchemaConstraint,
			includeNeo4jUIDLookupIndexes:   true,
			includeInfraSearchKeyIndex:     true,
			includeRetirementLookupIndexes: true,
			retiredConstraints:             neo4jRetiredUniqueConstraints,
			retiredConstraintPathIndexes:   neo4jRetiredConstraintPathIndexes,
		}, nil
	case SchemaBackendNornicDB:
		return schemaDialect{
			backend:                   normalized,
			constraint:                nornicDBSchemaConstraint,
			includeMergeLookupIndexes: true,
			retiredConstraints:        nornicDBRetiredUniqueConstraints,
			dropUIDLookupIndexes:      true,
		}, nil
	}
	return schemaDialect{}, fmt.Errorf("unsupported schema backend %q", backend)
}

// fulltextForms returns the full-text index statements to attempt in order
// (#7675). Neo4j runs only the modern CREATE FULLTEXT INDEX form: the
// procedure API was removed in Neo4j 5.0, so attempting it wastes a round
// trip on every apply. NornicDB runs only the procedure form.
func (d schemaDialect) fulltextForms(ft fulltextIndex) []string {
	if d.backend == SchemaBackendNornicDB {
		return []string{ft.primary}
	}
	return []string{ft.fallback}
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
	if isNeo4jRetiredConstraint(cypher) {
		return ""
	}
	return cypher
}

func nornicDBSchemaConstraint(cypher string) string {
	if isNornicDBRetiredConstraint(cypher) {
		return ""
	}
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
