// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package search

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/graph"
)

// This file owns the one writer clause that maintains the #7245 infra search
// key. Every statement that writes a searched field on an infra search label
// goes through Statement, so no writer computes the key its own
// way; TestInfraSearchKeyCensus fails when one does not.
//
// The key joins the node's own searched properties in Cypher, inside the same
// statement that writes them, so it can never disagree with the fields. It is
// a superset filter for the free-text predicate
// (deployment.InfraResourceFreeTextPredicate): every field value is in the
// key verbatim, so a query the predicate matches is a substring of the key,
// and the reader re-applies the predicate to every candidate.
//
// Measured on the pinned neo4j:2026-community (2026.08.1): `null IS :: STRING`
// is true, so each part guards with IS :: STRING NOT NULL; size() counts code
// points. The pinned NornicDB image does not evaluate the type predicate (it
// stores the expression text), so the NornicDB dialect omits the clause.

// Dialect selects whether graph writers append the infra search
// key clause. The zero value is invalid on purpose: production wiring must
// resolve it from the configured graph backend with
// DialectForBackend and pass it to a WithDialect constructor.
type Dialect uint8

const (
	// On appends the clause. It is the Neo4j dialect and the
	// shape the default constructors emit.
	On Dialect = iota + 1
	// Off leaves every statement byte-identical to the
	// statement without the key. It is the NornicDB dialect.
	Off
)

// Valid reports whether d is one of the declared dialects.
func (d Dialect) Valid() bool {
	return d == On || d == Off
}

// String returns the dialect name for logs and errors.
func (d Dialect) String() string {
	switch d {
	case On:
		return "infra_search_key_on"
	case Off:
		return "infra_search_key_off"
	}
	return "infra_search_key_invalid(" + strconv.Itoa(int(d)) + ")"
}

// DialectForBackend maps the configured graph backend, the same
// value that selects the schema dialect, to the writer dialect: Neo4j writes
// the key, NornicDB does not. Any other value is an error. Writing binaries
// pass graph.SchemaBackend(runtimecfg.LoadGraphBackend(...)); the two value
// sets are pinned equal by TestRuntimeGraphBackendsMatchSchemaBackends. This
// package must not import internal/runtime: runtime's tests import
// storage/cypher, which imports this package.
func DialectForBackend(backend graph.SchemaBackend) (Dialect, error) {
	switch backend {
	case graph.SchemaBackendNeo4j:
		return On, nil
	case graph.SchemaBackendNornicDB:
		return Off, nil
	}
	return 0, fmt.Errorf("infra search key dialect: unsupported graph backend %q", backend)
}

// MustDialect panics when a WithDialect constructor receives
// the zero or an unknown dialect. That is a wiring bug, and failing at
// construction is louder than writing a graph whose keys silently disagree
// with its fields.
func MustDialect(d Dialect, writer string) {
	if !d.Valid() {
		panic(fmt.Sprintf("%s: invalid %s; resolve it with DialectForBackend", writer, d))
	}
}

// separator joins the parts. It is a Cypher escape for the
// unit separator, a code point no searched field carries; a query that spans
// it can only widen the candidate set, which the reader's predicate re-check
// absorbs.
const separator = `'\u001F'`

// fields are the searched properties in predicate order; the
// resource type appears once as the coalesce the predicate reads.
var fields = []string{
	"name", "id", "kind", "resource_type", "data_type", "arn", "resource_id",
	"service_kind", "account_id", "region", "source", "config_path",
}

// Fields returns the twelve node properties the key reads, the
// same set deployment.InfraResourceFreeTextPredicate matches.
func Fields() []string {
	return append([]string(nil), fields...)
}

// joinedExpr is the Cypher expression for the joined key of
// alias. Each part is the property when it is a non-null string and the
// empty string when it is absent or not a string (the predicate's CONTAINS never matches a
// non-string), so no part can fail the write.
func joinedExpr(alias string) string {
	exprs := []string{
		alias + ".name",
		alias + ".id",
		alias + ".kind",
		"coalesce(" + alias + ".resource_type, " + alias + ".data_type)",
		alias + ".arn",
		alias + ".resource_id",
		alias + ".service_kind",
		alias + ".account_id",
		alias + ".region",
		alias + ".source",
		alias + ".config_path",
	}
	parts := make([]string, len(exprs))
	for i, e := range exprs {
		parts[i] = "CASE WHEN " + e + " IS :: STRING NOT NULL THEN " + e + " ELSE '' END"
	}
	return strings.Join(parts, " + "+separator+" + ")
}

// overflowFlagExpr is the Cypher value of infra_search_ovf for a joined key
// expression (a row variable in Clause and the backfill):
// graph.InfraSearchOverflowYes past the bound, graph.InfraSearchOverflowNo
// otherwise. The flag representation lives here and in the graph constants
// only.
func overflowFlagExpr(joined string) string {
	return "CASE WHEN size(" + joined + ") > " + strconv.Itoa(graph.InfraSearchKeyMaxChars) +
		" THEN '" + graph.InfraSearchOverflowYes + "' ELSE '" + graph.InfraSearchOverflowNo + "' END"
}

// keyValueExpr is the Cypher value of infra_search_key for a joined key: the
// key when it fits graph.InfraSearchKeyMaxChars, null otherwise.
func keyValueExpr(joined string) string {
	return "CASE WHEN size(" + joined + ") > " + strconv.Itoa(graph.InfraSearchKeyMaxChars) +
		" THEN null ELSE " + joined + " END"
}

// joinedVar is the row variable Clause binds the joined key to. Writer
// statements never use this name, and the backfill names its own copy
// differently, so it never shadows an outer variable.
const joinedVar = "infra_search_joined_key"

// Clause returns the clause that maintains the key on alias. It computes the
// joined key once into a row variable (WITH alias, <joined> AS
// infra_search_joined_key), then SETs both properties from it.
// infra_search_ovf is always written; the key is written only when it fits
// graph.InfraSearchKeyMaxChars and is null otherwise. A key past the bound
// would be one FULLTEXT term over Lucene's 32,766-byte limit, which fails the
// commit and leaves the index FAILED, so the bound is not optional. The
// joined value lives only in the row variable: no over-cap value is ever
// written to the node. A node with a 70 KB config_path is still written and
// is found through the flag.
//
// Computing the key once costs +17 db hits per row on a 500-row batch,
// against +41 for evaluating the join three times inline (PROFILE on the
// pinned Neo4j, docs/internal/evidence/7245-infra-search-key-dark.md). The
// clause ends the statement: its WITH keeps only alias in scope, so it must
// be appended last, which Statement does.
func Clause(alias string) string {
	return "WITH " + alias + ", " + joinedExpr(alias) + " AS " + joinedVar +
		"\nSET " + alias + "." + graph.InfraSearchOverflowProperty + " = " + overflowFlagExpr(joinedVar) +
		", " + alias + "." + graph.InfraSearchKeyProperty + " = " + keyValueExpr(joinedVar)
}

// Statement returns cypher with the key clause for alias
// appended when the dialect writes keys and label is an infra search label.
// The clause goes at the end: every writer statement leaves its node variable
// in scope there. Off returns cypher unchanged, byte for byte.
func Statement(dialect Dialect, label, alias, cypher string) string {
	if dialect == Off || !graph.IsInfraSearchKeyLabel(label) {
		return cypher
	}
	return cypher + "\n" + Clause(alias)
}
