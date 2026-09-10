// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// A graph fake that evaluates the emitted pattern, for the #5167 code-family
// graph reads.
//
// The batch-1 graph proofs started out as text-capture tests: run the route,
// keep the statement, assert the grant predicate appears somewhere in it. That
// class of test cannot see WHERE the predicate is attached, and clause
// attachment is the whole tenancy question. `MATCH (e:Function) OPTIONAL MATCH
// (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository) WHERE <grant>`
// contains the grant string and filters nothing: the predicate constrains the
// optional pattern, so a Function whose Repository fails the grant still comes
// back with the repository columns null.
//
// EvaluatingRepositoryGraph models that difference. It reads the statement far
// enough to answer two questions -- is the Repository binding optional, and
// which repository predicates govern it -- then applies Cypher's own semantics
// to seeded two-tenant rows. Move the grant back onto an OPTIONAL MATCH and the
// out-of-grant row reappears in the response body, which is what the route
// tests assert against.
//
// It is deliberately narrow. It understands the three repository predicate
// shapes this package emits and nothing else, and every seeded row is built to
// satisfy the non-repository predicates, so an unrecognised predicate is
// ignored rather than guessed at.
//
// It lives here, not in a package query or codequery _test.go file, because
// the #5167 batch-1 (CodeHandler complexity/quality/call-graph-metrics),
// batch-2a (LanguageQueryHandler), and batch-2b (CodeHandler call-chain)
// proofs share it, and CodeHandler moved to internal/query/codequery for
// #6060 while LanguageQueryHandler stayed in package query -- a _test.go
// symbol is not importable across that boundary.

// GraphGrantSeed is one row the fake can return, tagged with the repository
// its Repository anchor resolves to. An empty RepoID is a row the graph
// cannot attribute to any repository -- the row an OPTIONAL MATCH keeps and a
// required MATCH drops.
type GraphGrantSeed struct {
	RepoID string
	Row    map[string]any
}

// EvaluatingRepositoryGraph is a GraphQuery whose answers depend on where the
// grant predicate sits in the statement it is handed.
type EvaluatingRepositoryGraph struct {
	Seeds []GraphGrantSeed
	// RepositoryColumns are the projected columns that come from the
	// Repository/File side of the pattern. An OPTIONAL MATCH that fails nulls
	// exactly these and keeps the rest.
	RepositoryColumns []string
	// RepositoryAlias is the variable the statement binds its Repository node
	// to. The code-family builders are not consistent about it -- the batch-1
	// routes write `repo`, the language-query builders write `r` -- and the
	// alias is what locates both the binding clause and the predicates
	// governing it. Empty means `repo`.
	RepositoryAlias string
	Statements      []string
}

// alias returns the Repository variable this fake reads the statement against.
func (g *EvaluatingRepositoryGraph) alias() string {
	if g.RepositoryAlias == "" {
		return DefaultRepositoryGrantAlias
	}
	return g.RepositoryAlias
}

// Run satisfies querycontract.GraphQuery.
func (g *EvaluatingRepositoryGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.Statements = append(g.Statements, cypher)
	return g.evaluate(cypher, params), nil
}

// RunSingle satisfies querycontract.GraphQuery.
func (g *EvaluatingRepositoryGraph) RunSingle(
	_ context.Context,
	cypher string,
	params map[string]any,
) (map[string]any, error) {
	rows := g.evaluate(cypher, params)
	g.Statements = append(g.Statements, cypher)
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

func (g *EvaluatingRepositoryGraph) evaluate(cypher string, params map[string]any) []map[string]any {
	optional := RepositoryBindingIsOptionalForAlias(cypher, g.alias())
	predicates := RepositoryGoverningPredicatesForAlias(cypher, g.alias())
	rows := make([]map[string]any, 0, len(g.Seeds))
	for _, seed := range g.Seeds {
		admitted := seed.RepoID != ""
		for _, predicate := range predicates {
			if !RepositoryPredicateAdmitsForAlias(predicate, g.alias(), seed.RepoID, params) {
				admitted = false
				break
			}
		}
		switch {
		case admitted:
			rows = append(rows, cloneGraphRow(seed.Row))
		case optional:
			// The optional pattern did not match, so the Function row survives
			// with every column the pattern would have bound set to null.
			row := cloneGraphRow(seed.Row)
			for _, column := range g.RepositoryColumns {
				row[column] = nil
			}
			rows = append(rows, row)
		}
	}
	return rows
}

func cloneGraphRow(row map[string]any) map[string]any {
	clone := make(map[string]any, len(row))
	for key, value := range row {
		clone[key] = value
	}
	return clone
}

// DefaultRepositoryGrantAlias is the Repository variable the batch-1
// code-family builders bind. The alias-free helpers below read a statement
// against it.
const DefaultRepositoryGrantAlias = "repo"

// RepositoryBindingIsOptional reports whether the clause that binds the
// Repository alias is an OPTIONAL MATCH. Everything downstream of that answer
// is standard Cypher: an OPTIONAL MATCH's WHERE constrains the optional
// pattern, never the driving row set.
func RepositoryBindingIsOptional(cypher string) bool {
	return RepositoryBindingIsOptionalForAlias(cypher, DefaultRepositoryGrantAlias)
}

// RepositoryBindingIsOptionalForAlias is RepositoryBindingIsOptional for an
// explicit Repository alias.
func RepositoryBindingIsOptionalForAlias(cypher, alias string) bool {
	normalized := querycontract.NormalizeCypherWhitespace(cypher)
	anchor := strings.Index(normalized, alias+":Repository")
	if anchor < 0 {
		return false
	}
	prefix := normalized[:anchor]
	clause := strings.LastIndex(prefix, "MATCH")
	if clause < 0 {
		return false
	}
	return strings.HasSuffix(strings.TrimSpace(prefix[:clause]), "OPTIONAL")
}

// RepositoryGoverningPredicatesForAlias returns the predicates of the WHERE
// attached to the clause that binds the Repository alias -- the first WHERE
// after that binding, up to the next clause keyword. A predicate list joined by
// AND is what every builder in this family emits.
func RepositoryGoverningPredicatesForAlias(cypher, alias string) []string {
	normalized := querycontract.NormalizeCypherWhitespace(cypher)
	anchor := strings.Index(normalized, alias+":Repository")
	if anchor < 0 {
		return nil
	}
	rest := normalized[anchor:]
	start := strings.Index(rest, "WHERE ")
	if start < 0 {
		return nil
	}
	block := rest[start+len("WHERE "):]
	if end := clauseTerminatorIndex(block); end >= 0 {
		block = block[:end]
	}
	predicates := strings.Split(block, " AND ")
	for i := range predicates {
		predicates[i] = strings.TrimSpace(predicates[i])
	}
	return predicates
}

// clauseTerminatorIndex returns where the WHERE block ends -- the first clause
// keyword that starts a new clause.
//
// A plain substring scan for " WITH " is wrong: `f.name ENDS WITH '.go'`, the
// extension filter the language-query builders spliced into their WHERE until
// #6546, ended the block halfway through its first predicate and hid every
// predicate after it, including the grant. STARTS WITH has the same shape, and
// other builders may still use either. Only a WITH that is not the tail of one
// of those operators is a clause boundary.
func clauseTerminatorIndex(block string) int {
	terminators := []string{
		" OPTIONAL MATCH ", " MATCH ", " WITH ", " RETURN ", " ORDER BY ", " SKIP ", " LIMIT ",
	}
	earliest := -1
	for _, terminator := range terminators {
		for offset := 0; offset < len(block); {
			found := strings.Index(block[offset:], terminator)
			if found < 0 {
				break
			}
			at := offset + found
			offset = at + 1
			if terminator == " WITH " && isStringOperatorWith(block[:at]) {
				continue
			}
			if earliest < 0 || at < earliest {
				earliest = at
			}
			break
		}
	}
	return earliest
}

// isStringOperatorWith reports whether the text immediately before a " WITH "
// makes it the second word of ENDS WITH or STARTS WITH rather than a clause.
func isStringOperatorWith(prefix string) bool {
	trimmed := strings.TrimSpace(prefix)
	return strings.HasSuffix(trimmed, "ENDS") || strings.HasSuffix(trimmed, "STARTS")
}

// RepositoryPredicateAdmitsForAlias evaluates one repository predicate against
// a row whose Repository anchor resolved to repoID. A predicate this fake does
// not recognise admits the row: seeded rows are built to satisfy every
// non-repository predicate the builders emit, so guessing at those would
// invent a filter the backend does not apply.
func RepositoryPredicateAdmitsForAlias(predicate, alias, repoID string, params map[string]any) bool {
	switch {
	case strings.Contains(predicate, "$allowed_repository_ids"),
		strings.Contains(predicate, "$allowed_scope_ids"):
		return querycontract.GraphParamContains(params, "allowed_repository_ids", repoID) ||
			querycontract.GraphParamContains(params, "allowed_scope_ids", repoID)
	case strings.Contains(predicate, alias+".id = $repo_id"):
		bound, _ := params["repo_id"].(string)
		return repoID == bound && repoID != ""
	default:
		return true
	}
}
