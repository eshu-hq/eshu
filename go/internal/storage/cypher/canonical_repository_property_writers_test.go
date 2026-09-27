// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// #7285 Test C. The projector no longer DETACH DELETEs the Repository node on
// a re-projection, so the node's property map is never reset: a property a
// writer sets and later stops setting would persist forever. The
// projector-owned property set is exactly what canonicalNodeRepositoryUpsertCypher
// SETs, and that upsert rewrites every one of them on every attempt. This
// guard pins the invariant that makes the MERGE + SET upsert sufficient: no
// Cypher string literal under go/internal or go/cmd SETs a Repository
// property outside that owned set, or replaces or merges the whole property
// map. A writer that needs a new Repository property must add it to the
// projector upsert (so re-projection refreshes it) or switch the upsert to a
// full-replace `SET r = {...}` proven on both backends.
//
// This is a literal scan of single Go string literals, not proof that no other
// Repository property writer exists. It binds a variable to Repository through
// a node pattern `(r:Repository`, a label predicate `WHERE r:Repository`, and
// `WITH r AS alias` chains, and flags `SET r.p = ...`, `SET r.p += ...`,
// `SET r[$key] = ...`, `SET r = ...` and `SET r += ...` outside the owned set.
// It does not see:
//
//   - Cypher split across a Go string concatenation (`"...(r:Repository) " +
//     "SET r.p = 1"`): each literal is scanned alone;
//   - a label that is not a literal, such as `fmt.Sprintf("(r:%s)", label)` or
//     a `%s` label placeholder filled from a constant;
//   - a Repository variable rebound through `UNWIND`, `collect` or a list
//     comprehension (`WITH collect(r) AS repos UNWIND repos AS repo SET
//     repo.p = 1`): only `WITH r AS alias` chains are followed;
//   - a write through a procedure (`CALL apoc.create.setProperty(r, ...)`) or a
//     Repository reached with no label at all (`MATCH (n {id: $repo_id})`);
//   - Cypher in _test.go files, testdata, or outside go/internal and go/cmd.
//
// The live half, TestLiveRepositoryRetryRefreshesProjectorProperties, pins the
// current property map against a fresh projection on a real backend.

var (
	// repositoryBindingPattern binds a variable to the Repository label in a
	// node pattern `(r:Repository` and in a label predicate `WHERE r:Repository`.
	repositoryBindingPattern = regexp.MustCompile(`\b(\w+):Repository\b`)
	cypherSetClausePattern   = regexp.MustCompile(`(?i)\bSET\b`)
	cypherClauseEndPattern   = regexp.MustCompile(`(?i)\b(MERGE|MATCH|WITH|RETURN|CREATE|DELETE|DETACH|UNWIND|OPTIONAL|FOREACH|CALL|REMOVE|ON|SET|WHERE|UNION|LIMIT|ORDER)\b`)
	upsertOwnedPropertyRegex = regexp.MustCompile(`\br\.(\w+)\s*=`)
)

// repositoryOwnedProperties parses the projector upsert's SET list.
func repositoryOwnedProperties(t *testing.T) map[string]bool {
	t.Helper()
	owned := map[string]bool{"id": true}
	for _, match := range upsertOwnedPropertyRegex.FindAllStringSubmatch(canonicalNodeRepositoryUpsertCypher, -1) {
		owned[match[1]] = true
	}
	for _, want := range []string{"name", "path", "scope_id", "generation_id", "evidence_source"} {
		if !owned[want] {
			t.Fatalf("owned-property parse of the upsert lost %q; parsed %v", want, owned)
		}
	}
	return owned
}

// repositoryBindings returns every variable cypherText binds to a Repository
// node, directly or through a `WITH v AS alias` chain, in first-seen order.
func repositoryBindings(cypherText string) []string {
	seen := map[string]bool{}
	var bindings []string
	add := func(variable string) {
		if !seen[variable] {
			seen[variable] = true
			bindings = append(bindings, variable)
		}
	}
	for _, binding := range repositoryBindingPattern.FindAllStringSubmatch(cypherText, -1) {
		add(binding[1])
	}
	for i := 0; i < len(bindings); i++ {
		alias := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(bindings[i]) + `\s+AS\s+(\w+)\b`)
		for _, match := range alias.FindAllStringSubmatch(cypherText, -1) {
			add(match[1])
		}
	}
	return bindings
}

// repositoryPropertyViolations returns every SET on a Repository-bound
// variable in cypherText that writes a property outside owned (`SET v.p = ...`
// or `SET v.p += ...`), writes a dynamic property (`SET v[$key] = ...`), or
// assigns the whole map (`SET v = ...` / `SET v += ...`).
func repositoryPropertyViolations(cypherText string, owned map[string]bool) []string {
	var violations []string
	for _, binding := range repositoryBindings(cypherText) {
		variable := regexp.QuoteMeta(binding)
		propertyWrite := regexp.MustCompile(`\b` + variable + `\.(\w+)\s*\+?=[^=~]`)
		mapWrite := regexp.MustCompile(`\b` + variable + `\s*\+?=[^=~]`)
		dynamicWrite := regexp.MustCompile(`\b` + variable + `\s*\[`)
		for _, set := range cypherSetClausePattern.FindAllStringIndex(cypherText, -1) {
			segment := cypherText[set[1]:]
			if end := cypherClauseEndPattern.FindStringIndex(segment); end != nil {
				segment = segment[:end[0]]
			}
			segment += " "
			for _, write := range propertyWrite.FindAllStringSubmatch(segment, -1) {
				if !owned[write[1]] {
					violations = append(violations, binding+"."+write[1])
				}
			}
			if mapWrite.MatchString(segment) {
				violations = append(violations, binding+" (whole property map)")
			}
			if dynamicWrite.MatchString(segment) {
				violations = append(violations, binding+"[dynamic property]")
			}
		}
	}
	return violations
}

func TestRepositoryPropertyWritersStayInsideTheProjectorUpsert(t *testing.T) {
	t.Parallel()

	owned := repositoryOwnedProperties(t)
	goRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve go module root: %v", err)
	}
	var violations []string
	literals := 0
	for _, dir := range []string{"internal", "cmd"} {
		walkErr := filepath.WalkDir(filepath.Join(goRoot, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" || entry.Name() == "vendor" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if parseErr != nil {
				return parseErr
			}
			ast.Inspect(file, func(node ast.Node) bool {
				lit, ok := node.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING || !strings.Contains(lit.Value, ":Repository") {
					return true
				}
				text, unquoteErr := strconv.Unquote(lit.Value)
				if unquoteErr != nil {
					return true
				}
				literals++
				rel, _ := filepath.Rel(goRoot, path)
				for _, violation := range repositoryPropertyViolations(text, owned) {
					violations = append(violations, rel+": "+violation)
				}
				return true
			})
			return nil
		})
		if walkErr != nil {
			t.Fatalf("scan %s: %v", dir, walkErr)
		}
	}
	if literals < 50 {
		t.Fatalf("scanned %d Repository Cypher literals; the scan is not reaching the tree", literals)
	}
	sort.Strings(violations)
	if len(violations) > 0 {
		t.Fatalf("Repository properties written outside the projector upsert's owned set %v "+
			"(they would persist forever now that re-projection keeps the node, #7285):\n%s",
			owned, strings.Join(violations, "\n"))
	}
}

// TestRepositoryPropertyViolationsDetectsSeededWriters is the guard's
// seeded-violation RED/GREEN pair.
func TestRepositoryPropertyViolationsDetectsSeededWriters(t *testing.T) {
	t.Parallel()

	owned := repositoryOwnedProperties(t)
	cases := []struct {
		name  string
		text  string
		wants []string
	}{
		{"stray property", "MERGE (repo:Repository {id: $id})\nSET repo.legacy_flag = true", []string{"repo.legacy_flag"}},
		{"stray property after owned", "MATCH (r:Repository {id: $id}) SET r.name = $name, r.is_archived = $a", []string{"r.is_archived"}},
		{"map replace", "MERGE (r:Repository {id: $id}) SET r = $props", []string{"r (whole property map)"}},
		{"map merge", "MERGE (r:Repository {id: $id}) SET r += $props", []string{"r (whole property map)"}},
		{"on create stray", "MERGE (t:Repository {id: $id}) ON CREATE SET t.first_seen = $now", []string{"t.first_seen"}},
		{"projector upsert", canonicalNodeRepositoryUpsertCypher, nil},
		{"reducer stub", "MERGE (target_repo:Repository {id: row.target_repo_id})\nON CREATE SET target_repo.evidence_source = $evidence_source, target_repo.generation_id = $generation_id", nil},
		{"where equality", "MATCH (r:Repository) WHERE r.id = $id AND r.legacy = $x RETURN r", nil},
		{"relationship set", "MATCH (r:Repository {id: $id})-[rel:DEFINES]->(w) SET rel.confidence = 1.0, w.name = $n", nil},
		// #7285 review F3: evasions the first version of the scan missed.
		{"property increment", "MATCH (r:Repository {id: $id}) SET r.sync_count += 1", []string{"r.sync_count"}},
		{"aliased binding", "MATCH (r:Repository {id: $id}) WITH r AS repo SET repo.legacy_flag = true", []string{"repo.legacy_flag"}},
		{"chained alias", "MATCH (r:Repository {id: $id}) WITH r AS a WITH a AS b SET b += $props", []string{"b (whole property map)"}},
		{"label predicate", "MATCH (r {id: $id}) WHERE r:Repository SET r.legacy_flag = true", []string{"r.legacy_flag"}},
		{"dynamic property", "MATCH (r:Repository {id: $id}) SET r[$key] = $value", []string{"r[dynamic property]"}},
		{"aliased owned property", "MATCH (r:Repository {id: $id}) WITH r AS repo SET repo.name = $name", nil},
		{"alias of another label", "MATCH (r:Repository {id: $id})-[:DEFINES]->(w) WITH w AS workload SET workload.flag = true", nil},
	}
	for _, tc := range cases {
		got := repositoryPropertyViolations(tc.text, owned)
		if strings.Join(got, ",") != strings.Join(tc.wants, ",") {
			t.Errorf("%s: violations = %v, want %v", tc.name, got, tc.wants)
		}
	}
}
