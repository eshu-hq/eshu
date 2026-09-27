// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// tierLabelPattern matches a Cypher `:Tier` label token. It is
// case-sensitive -- a lowercase `tier` property key such as
// `{tier:$tier}` never matches -- and the trailing \b excludes
// `:TierPolicy` (a word-character boundary means "Tier" immediately
// followed by "P" is not a match). The label may be backtick-quoted.
var tierLabelPattern = regexp.MustCompile(":`?Tier`?\\b")

// tierSetLabelPattern flags the dynamic-label write shape `SET n:Tier`
// (Cypher's runtime add-a-label form). mergeOpenPattern and
// createClausePattern (merge_then_create_repo_scan_test.go) cannot see this
// shape: it opens with neither `MERGE(` nor `CREATE(`.
var tierSetLabelPattern = regexp.MustCompile(`(?i)\bSET\b[^;]*:` + "`?Tier`?" + `\b`)

// writesTierLabel reports whether value contains a ';'-delimited Cypher
// statement that both mentions the :Tier label and opens a node pattern
// with MERGE( or CREATE(, or dynamically adds the label with `SET n:Tier`.
// This is deliberately conservative: a statement that MATCHes an existing
// :Tier node and then MERGEs a relationship off it (the CONTAINS-membership
// writer shape this guard exists to catch) counts as a hit even though the
// MERGE itself opens on a different variable, because the same statement
// still declares and mutates Tier membership.
//
// A pure MATCH/RETURN read of :Tier never matches (no MERGE/CREATE/SET
// keyword present). The `CREATE CONSTRAINT ... FOR (t:Tier) REQUIRE ...` DDL
// never matches either: after the CREATE keyword the next token is
// CONSTRAINT, not "(" (or a named-path binding followed by "="), so
// createClausePattern does not match it.
func writesTierLabel(value string) bool {
	for _, statement := range strings.Split(value, ";") {
		if !tierLabelPattern.MatchString(statement) {
			continue
		}
		if mergeOpenPattern.MatchString(statement) ||
			createClausePattern.MatchString(statement) ||
			tierSetLabelPattern.MatchString(statement) {
			return true
		}
	}
	return false
}

// tierSingleMembershipWriters names "path:line" locations (relative to the
// go module root, as produced by scanForTierWriters) of the only writers
// permitted to touch the :Tier label or a (:Tier)-[:CONTAINS]->(:Repository)
// edge. Adding an entry here costs three things, all required:
//
//   - (i) the writer retracts any existing (:Tier)-[:CONTAINS]->(repo) edge
//     from a DIFFERENT Tier before its own MERGE, so a repository can never
//     end up counted under two tiers at once;
//   - (ii) a live test proves that writing repository R into tier A and then
//     into tier B leaves exactly one CONTAINS edge pointing into R;
//   - (iii) the enrichBlastRadiusTiers query-source-coverage row (#6590,
//     go/internal/queryplan/testdata/query-source-coverage.yaml) names this
//     writer as the code enforcer behind its fan_out_multiplier: 1.
//
// Empty today: no :Tier writer exists in-tree (ops-qa has 0 Tier nodes and 0
// CONTAINS edges into a Tier), so enrichBlastRadiusTiers's "one tier per
// repo" assumption holds vacuously. This test fails the build the moment a
// :Tier writer appears without paying that price, instead of the assumption
// silently going wrong.
var tierSingleMembershipWriters = map[string]bool{}

// tierDynamicLabelAllowlist names "path:line" locations of a Go string
// literal whose value is exactly "Tier" that is NOT a dynamic Cypher label
// write. The one entry today is an AWS tag-key vocabulary list, not Cypher.
var tierDynamicLabelAllowlist = map[string]bool{
	"internal/replay/recordpseudo/walker.go:17": true,
}

// scanForTierWriters walks every non-_test.go file under go/cmd and
// go/internal (the same driver as scanForNodeMergeThenCreate in
// merge_then_create_repo_scan_test.go, duplicated rather than shared so this
// guard's tree walk stays independent of that one's), and returns:
//   - writeHits: "path:line" sites of an un-allowlisted writesTierLabel hit;
//   - dynamicHits: "path:line" sites of an un-allowlisted Go string literal
//     whose value is exactly "Tier";
//   - tierMentions: how many scanned string values matched tierLabelPattern
//     at all, write or read -- this must be > 0 for the scan to have
//     provably reached real Cypher text (the blast_radius.go read and the
//     schema_tables.go constraint), rather than walked an empty tree;
//   - scannedFiles: how many .go files were inspected.
func scanForTierWriters(t *testing.T) (writeHits, dynamicHits []string, tierMentions, scannedFiles int) {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	// thisFile is go/internal/storage/cypher/tier_writer_scan_test.go; the go
	// module root is three directories up.
	goModuleRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))

	fset := token.NewFileSet()
	for _, top := range []string{"cmd", "internal"} {
		root := filepath.Join(goModuleRoot, top)
		walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			scannedFiles++

			file, parseErr := parser.ParseFile(fset, path, nil, 0)
			if parseErr != nil {
				return fmt.Errorf("parse %s: %w", path, parseErr)
			}
			relPath, relErr := filepath.Rel(goModuleRoot, path)
			if relErr != nil {
				return relErr
			}

			identMap := buildFileConstIdentMap(file)
			record := func(pos token.Pos, value string) {
				if tierLabelPattern.MatchString(value) {
					tierMentions++
				}
				site := fmt.Sprintf("%s:%d", relPath, fset.Position(pos).Line)
				if writesTierLabel(value) && !tierSingleMembershipWriters[site] {
					writeHits = append(writeHits, site)
				}
				if value == "Tier" && !tierDynamicLabelAllowlist[site] {
					dynamicHits = append(dynamicHits, site)
				}
			}

			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.BinaryExpr:
					if node.Op != token.ADD {
						return true
					}
					value, foldOK := foldStringExpr(node, identMap)
					if !foldOK {
						return true
					}
					record(node.Pos(), value)
					return false
				case *ast.BasicLit:
					if node.Kind != token.STRING {
						return true
					}
					value, unquoteErr := strconv.Unquote(node.Value)
					if unquoteErr != nil {
						return true
					}
					record(node.Pos(), value)
					return true
				}
				return true
			})
			return nil
		})
		if walkErr != nil {
			t.Fatalf("filepath.Walk(%s): %v", root, walkErr)
		}
	}
	return writeHits, dynamicHits, tierMentions, scannedFiles
}

// TestNoTierWriterWithoutSingleMembershipContract is the #6590 fail-closed
// guard behind enrichBlastRadiusTiers's fan_out_multiplier: 1: per-repo
// (tier, risk) multiplicity is guaranteed by the write path or schema, never
// by the reader, and today no :Tier writer exists, so 1 is only vacuously
// true. This test fails the moment a :Tier writer or a dynamic "Tier" label
// literal appears anywhere under go/cmd or go/internal without a matching
// tierSingleMembershipWriters or tierDynamicLabelAllowlist entry.
func TestNoTierWriterWithoutSingleMembershipContract(t *testing.T) {
	t.Parallel()

	writeHits, dynamicHits, tierMentions, scannedFiles := scanForTierWriters(t)
	if scannedFiles == 0 {
		t.Fatal("scanned zero .go files under go/cmd and go/internal -- the scan itself is broken, not proof there is nothing to check")
	}
	if tierMentions == 0 {
		t.Fatal("found zero :Tier label mentions across go/cmd and go/internal -- expected at least the blast_radius.go tier-lookup read and the schema_tables.go tier_name constraint, so the walk is not reaching real files")
	}
	if len(writeHits) > 0 {
		t.Fatalf(
			"found %d :Tier write(s) with no registered single-membership contract in tierSingleMembershipWriters "+
				"(#6590): a :Tier writer must retract any existing (:Tier)-[:CONTAINS]->(repo) edge from a "+
				"different Tier before its own MERGE, be proven live to leave exactly one CONTAINS edge into a "+
				"repo written into tier A then tier B, and be named as the enforcer in enrichBlastRadiusTiers's "+
				"query-source-coverage row before its fan_out_multiplier: 1 can stay true. Violations:\n  %s",
			len(writeHits), strings.Join(writeHits, "\n  "),
		)
	}
	if len(dynamicHits) > 0 {
		t.Fatalf(
			"found %d Go string literal(s) with value exactly \"Tier\" outside tierDynamicLabelAllowlist: a "+
				"dynamic Cypher label write (`SET n:$label` with this string bound to $label) is invisible to "+
				"the static MERGE/CREATE/SET-clause scan, so any new exact-\"Tier\" literal must be reviewed and "+
				"either allowlisted as non-Cypher or treated as a :Tier writer. Violations:\n  %s",
			len(dynamicHits), strings.Join(dynamicHits, "\n  "),
		)
	}
}

// TestWritesTierLabel is the unit-level proof for writesTierLabel: every RED
// input is a Cypher write shape that must be caught, and every GREEN input
// is a read, a differently-labeled write, or DDL that must not be.
func TestWritesTierLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{
			name:  "node MERGE with Tier label",
			value: `MERGE (t:Tier {name:$n})`,
			want:  true,
		},
		{
			name:  "node CREATE with Tier label",
			value: `CREATE (t:Tier)-[:CONTAINS]->(r)`,
			want:  true,
		},
		{
			name:  "match existing Tier then merge membership edge",
			value: `MATCH (r:Repository {id:$id}) MATCH (t:Tier {name:$t}) MERGE (t)-[:CONTAINS]->(r)`,
			want:  true,
		},
		{
			name:  "dynamic SET label",
			value: `MATCH (n) SET n:Tier`,
			want:  true,
		},
		{
			name:  "lowercase merge keyword across a newline",
			value: "merge\n(t:Tier)",
			want:  true,
		},
		{
			name:  "constraint DDL",
			value: `CREATE CONSTRAINT tier_name IF NOT EXISTS FOR (t:Tier) REQUIRE t.name IS UNIQUE`,
			want:  false,
		},
		{
			name:  "differently labeled merge",
			value: `MERGE (t:TierPolicy {name:$n, risk:$r})`,
			want:  false,
		},
		{
			name:  "tier as a property, not a label",
			value: `MERGE (r:Repository {tier:$tier})`,
			want:  false,
		},
		{
			name:  "pure read of Tier",
			value: `MATCH (a:Repository)<-[:CONTAINS]-(tier:Tier) RETURN a.id, tier.name`,
			want:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := writesTierLabel(test.value); got != test.want {
				t.Fatalf("writesTierLabel(%q) = %v, want %v", test.value, got, test.want)
			}
		})
	}
}
