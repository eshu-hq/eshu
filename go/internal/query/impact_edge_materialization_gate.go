// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"regexp"
	"strings"
)

// relationshipTypeTokenPattern matches a Cypher relationship pattern's bracket
// body -- "[:TYPE]", "[r:TYPE]", "[:A|B|C]", "[:TYPE*1..5]" -- and captures
// the pipe-separated type list. It intentionally does not match node-label
// brackets: those use parentheses ("(n:Label)"), never square brackets, in
// Cypher syntax, so this pattern cannot mistake a node label for a
// relationship type. A trailing variable-length quantifier ("*1..5") is not
// part of the identifier character class, so it is left unmatched and never
// pollutes the captured type list.
var relationshipTypeTokenPattern = regexp.MustCompile(
	`\[\s*(?:[A-Za-z_][A-Za-z0-9_]*)?\s*:\s*([A-Za-z_][A-Za-z0-9_]*(?:\s*\|\s*[A-Za-z_][A-Za-z0-9_]*)*)`,
)

// extractRelationshipTypeTokens returns every relationship-type token a
// Cypher string's relationship patterns name, in source order, including
// duplicates. It handles a bound or anonymous relationship variable
// ("-[:A]->", "-[r:A]->"), either arrow direction, a pipe-separated type
// union ("-[:A|B|C]->", split into "A","B","C"), and a variable-length
// quantifier ("-[:A*1..3]->", which does not affect extraction since "*" is
// outside the identifier character class). Node labels are out of scope (see
// package doc comment on TestImpactBlastRadiusQueriesOnlyTraverseDisclosedEdges).
func extractRelationshipTypeTokens(cypher string) []string {
	var tokens []string
	for _, match := range relationshipTypeTokenPattern.FindAllStringSubmatch(cypher, -1) {
		for _, token := range strings.Split(match[1], "|") {
			tokens = append(tokens, strings.TrimSpace(token))
		}
	}
	return tokens
}

// distinctStrings returns the unique values in values, preserving first-seen
// order.
func distinctStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// unmaterializedAnnotatedImpactEdgeTypes is the explicit escape hatch for an
// edge type traversed by an impact/blast-radius query that owns no
// coverage-edge-type list (so it has no coverage/complete API disclosure)
// and that EdgeMaterializationCoverage does not yet recognize as
// materialized, but that has been manually reviewed and accepted -- for
// example, a known future-writer edge tracked by its own issue. Empty today:
// every edge every impactBlastRadiusGateQueries entry traverses is either
// owned by that query's coverage-edge-type list or genuinely materialized
// per EdgeMaterializationCoverage. A new entry here is a deliberate,
// reviewed exception, not a place to silence a real gap -- prefer adding a
// coverage-edge-type list entry (if the target_type has one) or a real
// writer instead.
var unmaterializedAnnotatedImpactEdgeTypes = map[string]struct{}{}

// impactBlastRadiusGateQuery names one production Cypher constant in
// impact_blast_radius.go the #5335 edge-materialization gate audits.
type impactBlastRadiusGateQuery struct {
	// ConstName is the Go identifier of the `const ... = \`...\`` literal in
	// impact_blast_radius.go, verified present as a literal string constant
	// by TestImpactBlastRadiusGateQueriesAreLiteralConstants (the "LITERAL-ONLY
	// discipline" anti-false-green mitigation).
	ConstName string
	// Cypher is the constant's value, referenced directly (not re-parsed)
	// since it lives in the same package.
	Cypher string
	// CoverageEdgeTypes is the coverage-edge-type list this query's
	// target_type owns and discloses via the API response's coverage/complete
	// fields (sqlTableBlastRadiusEdgeTypes, crossplaneXrdBlastRadiusEdgeTypes).
	// Nil for a query whose target_type has no coverage list at all
	// (repository, terraform_module): every edge type such a query traverses
	// must instead be genuinely materialized (EdgeMaterializationCoverage) or
	// explicitly annotated (unmaterializedAnnotatedImpactEdgeTypes), since
	// there is no coverage/complete field to disclose an unwritten edge.
	CoverageEdgeTypes []string
	// MinDistinctEdgeTypes is the positive-extraction floor: the minimum
	// number of distinct relationship-type tokens this query must yield.
	// Seeded from today's known token count so a tokenizer regression that
	// silently drops tokens fails this floor instead of vacuously passing an
	// empty or partial extraction.
	MinDistinctEdgeTypes int
}

// impactBlastRadiusGateQueries is the closed list of production Cypher
// constants in impact_blast_radius.go this gate audits: the six
// target_type-scoped blast-radius queries feeding blastRadiusAffected's
// switch, plus the shared tier-lookup query. See the package-level doc
// comment on TestImpactBlastRadiusQueriesOnlyTraverseDisclosedEdges for the
// v1 scope limit (this file only, not every "impact" query in the package).
// impactBlastRadiusGateQueries audits the edge types traversed by the
// blast-radius queries. It builds each query with a NON-scoped access filter so
// the audited text is the base (grant-predicate-free) shape -- the #5167 W3 P1
// grant predicate only adds an `a.id/repo.id IN $...` node filter for scoped
// callers and traverses no additional edge types, so the edge-disclosure audit
// is identical for both shapes.
var impactBlastRadiusGateQueries = func() []impactBlastRadiusGateQuery {
	unscoped := repositoryAccessFilter{allScopes: true}
	return []impactBlastRadiusGateQuery{
		{ConstName: "blastRadiusRepositoryCypher", Cypher: blastRadiusRepositoryQuery(unscoped), MinDistinctEdgeTypes: 1},
		{ConstName: "blastRadiusTerraformSourceReposCypher", Cypher: blastRadiusTerraformSourceReposQuery(unscoped), MinDistinctEdgeTypes: 2},
		{ConstName: "blastRadiusDependentsByIDCypher", Cypher: blastRadiusDependentsByIDQuery(unscoped), MinDistinctEdgeTypes: 1},
		{ConstName: "blastRadiusCrossplaneCypher", Cypher: blastRadiusCrossplaneQuery(unscoped), CoverageEdgeTypes: crossplaneXrdBlastRadiusEdgeTypes, MinDistinctEdgeTypes: 3},
		{ConstName: "blastRadiusSqlTableCypher", Cypher: blastRadiusSqlTableQuery(unscoped), CoverageEdgeTypes: sqlTableBlastRadiusEdgeTypes, MinDistinctEdgeTypes: 9},
		{ConstName: "blastRadiusTierLookupCypher", Cypher: blastRadiusTierLookupCypher, MinDistinctEdgeTypes: 1},
	}
}()

// resolveImpactEdgeType reports whether edgeType, traversed by query, is
// disclosed rather than silent: owned by query's coverage-edge-type list
// (disclosed via the API coverage/complete fields either way), genuinely
// materialized per EdgeMaterializationCoverage, or explicitly annotated in
// unmaterializedAnnotatedImpactEdgeTypes.
func resolveImpactEdgeType(query impactBlastRadiusGateQuery, edgeType string) (ok bool, reason string) {
	for _, owned := range query.CoverageEdgeTypes {
		if owned == edgeType {
			return true, ""
		}
	}
	if EdgeMaterializationCoverage(edgeType).Materialized {
		return true, ""
	}
	if _, annotated := unmaterializedAnnotatedImpactEdgeTypes[edgeType]; annotated {
		return true, ""
	}
	return false, "edge type " + edgeType + " traversed by " + query.ConstName +
		" is neither in its coverage-edge-type list, materialized per EdgeMaterializationCoverage, " +
		"nor in unmaterializedAnnotatedImpactEdgeTypes -- add a writer and a coverage-edge-type " +
		"list entry, or explicitly annotate it as unmaterialized"
}
