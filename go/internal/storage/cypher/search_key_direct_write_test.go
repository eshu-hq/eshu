// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
)

// The second #7245 census. The FULLTEXT index makes the whole key one Lucene
// term, and a term past the 32,766-byte limit does not reject the row: it
// fails the commit after the node is written and leaves the index FAILED. Only
// search.Clause enforces the cap, so no production code may write
// infra_search_key or infra_search_ovf any other way: not in Cypher text
// (SET n.infra_search_key = ..., a map entry infra_search_key: ...) and not
// as a Go property-map key a SET n += map would carry. The search package that
// owns the clause and the backfill is the only exception.

var (
	directKeyAssign   = regexp.MustCompile(`\b\w+\.(` + graph.InfraSearchKeyProperty + `|` + graph.InfraSearchOverflowProperty + `)\s*=`)
	directKeyMapEntry = regexp.MustCompile(`[{,]\s*(` + graph.InfraSearchKeyProperty + `|` + graph.InfraSearchOverflowProperty + `)\s*:`)
	directKeyOwnerDir = "internal/storage/cypher/infra/search"
)

// scanDirectKeyWrites returns every production string that writes the key or
// flag outside search.Clause.
func (s *censusScanner) scanDirectKeyWrites() []censusFinding {
	var findings []censusFinding
	for _, f := range s.files {
		if filepath.ToSlash(f.dir) == directKeyOwnerDir {
			continue
		}
		ast.Inspect(f.file, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if v, err := strconv.Unquote(lit.Value); err == nil && (v == graph.InfraSearchKeyProperty || v == graph.InfraSearchOverflowProperty) {
					findings = append(findings, censusFinding{
						fmt.Sprintf("%s:%d", f.rel, f.fset.Position(n.Pos()).Line),
						"names " + v + " as a Go string; only search.Clause may write it", v,
					})
				}
			}
			if _, isIdent := n.(*ast.Ident); isIdent {
				return true
			}
			folded, ok := s.fold(f.dir, n, 0)
			if !ok {
				return true
			}
			text := folded.text
			for alias := range folded.keyed {
				text = strings.ReplaceAll(text, search.Clause(alias), "")
			}
			if m := directKeyAssign.FindStringSubmatch(text); m != nil {
				findings = append(findings, censusFinding{fmt.Sprintf("%s:%d", f.rel, f.fset.Position(n.Pos()).Line), "sets " + m[1] + " outside search.Clause", headOf(text)})
			} else if m := directKeyMapEntry.FindStringSubmatch(text); m != nil {
				findings = append(findings, censusFinding{fmt.Sprintf("%s:%d", f.rel, f.fset.Position(n.Pos()).Line), "writes " + m[1] + " in a map outside search.Clause", headOf(text)})
			}
			return false
		})
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].file < findings[j].file })
	return findings
}

func headOf(text string) string {
	head := strings.Join(strings.Fields(text), " ")
	if len(head) > 160 {
		head = head[:160]
	}
	return head
}

// TestInfraSearchKeyIsWrittenOnlyByTheClause fails when production code
// writes the key or flag without the capped clause.
func TestInfraSearchKeyIsWrittenOnlyByTheClause(t *testing.T) {
	t.Parallel()

	for _, f := range newCensusScanner(t, filepath.Join("..", "..", ".."), censusDirs).scanDirectKeyWrites() {
		t.Errorf("direct infra search key write: %s", f)
	}
}

// TestDirectKeyWriteCensusFlagsSeededViolations proves the second census can
// fail on each way around the clause and stays clean on keyed statements, on
// reads of the key, and on the owning package.
func TestDirectKeyWriteCensusFlagsSeededViolations(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		src := "package seed\n\nimport (\n\t\"github.com/eshu-hq/eshu/go/internal/graph\"\n\t\"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search\"\n)\n\nvar _ = graph.InfraSearchKeyProperty\nvar _ = search.On\n\n" + body + "\n"
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	q := func(s string) string { return "`" + s + "`" }

	// Violations.
	write("internal/storage/seed/set_key.go", "const setKey = "+q("UNWIND $rows AS row\nMERGE (r:CloudResource {uid: row.uid})\nSET r.infra_search_key = row.k"))
	write("internal/storage/seed/set_flag.go", "const setFlag = `MATCH (r:CloudResource) SET r.` + graph.InfraSearchOverflowProperty + ` = 'F'`")
	write("internal/storage/seed/map_entry.go", "const mapEntry = "+q("MERGE (r:CloudResource {uid: $uid, infra_search_key: $k})"))
	write("internal/reducer/seed/go_map.go", "func goMap(props map[string]any) { props[\"infra_search_key\"] = \"x\" }")

	// Clean.
	write("internal/storage/seed/keyed.go", "const keyedBase = "+q("UNWIND $rows AS row\nMERGE (r:CloudResource {uid: row.uid})\nSET r.name = row.name")+
		"\n\nfunc keyed(d search.Dialect) string { return search.Statement(d, \"CloudResource\", \"r\", keyedBase) }")
	write("internal/storage/seed/read.go", "const read = "+q("MATCH (r:CloudResource) WHERE r.infra_search_key CONTAINS $q RETURN r.uid"))
	write("internal/storage/cypher/infra/search/owner.go", "const owner = "+q("SET n.infra_search_key = null")+"\n\nvar ownerName = \"infra_search_ovf\"")

	findings := newCensusScanner(t, root, []string{"internal/storage", "internal/reducer"}).scanDirectKeyWrites()
	got := map[string]bool{}
	for _, f := range findings {
		got[filepath.Base(strings.SplitN(f.file, ":", 2)[0])] = true
	}
	for _, name := range []string{"set_key.go", "set_flag.go", "map_entry.go", "go_map.go"} {
		if !got[name] {
			t.Errorf("seeded violation %s not found; findings = %v", name, findings)
		}
	}
	for _, name := range []string{"keyed.go", "read.go", "owner.go"} {
		if got[name] {
			t.Errorf("%s flagged, want clean; findings = %v", name, findings)
		}
	}
}
