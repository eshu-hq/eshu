// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package goldengate

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

// writePathSourceRoots are the Go module trees, relative to this package,
// whose non-test string literals hold every graph write statement.
var writePathSourceRoots = []string{"../../internal", "../../cmd", "../../pkg"}

var (
	rowKeyRead = regexp.MustCompile(`\brow\.([A-Za-z_][A-Za-z0-9_]*)`)
	// dynamicRowKeyFormat finds a row key assembled by fmt; only the
	// `row.%s_%s` shape internal/graph/batch.go uses is covered, via the
	// snake_case rule.
	dynamicRowKeyFormat = regexp.MustCompile(`\brow\.%`)
	snakeRowKeyFormat   = regexp.MustCompile(`\brow\.%[a-z]_%[a-z]\b`)
	unwindBinding       = regexp.MustCompile(`(?i)\bUNWIND\s+\S+\s+AS\s+([A-Za-z_][A-Za-z0-9_]*)`)
)

// writePathLiterals returns the unquoted string literals of every non-test Go
// file under writePathSourceRoots, keyed by file path. Grouping by file lets
// a scan see an UNWIND and a dereference built in separate literals, as the
// orphan-sweep builders do.
func writePathLiterals(t *testing.T) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, root := range writePathSourceRoots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil {
						out[path] = append(out[path], s)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	if len(out) < 1000 {
		t.Fatalf("found string literals in only %d files; the source roots moved", len(out))
	}
	return out
}

// unmatchableDynamicRowKey reports whether lit is a fmt format that builds a
// `row.<key>` read at run time in a shape other than `row.%s_%s`.
func unmatchableDynamicRowKey(lit string) bool {
	return dynamicRowKeyFormat.MatchString(lit) && !snakeRowKeyFormat.MatchString(lit)
}

// TestUnmatchableDynamicRowKeySeeded is the seeded RED/GREEN pair for the
// dynamic row-key scan in TestWritePathRowKeysMatchSource.
func TestUnmatchableDynamicRowKeySeeded(t *testing.T) {
	t.Parallel()

	for lit, want := range map[string]bool{
		"%s: row.%s_%s":        false,
		"n.name = row.name":    false,
		"n.%s = row.%s":        true,
		"SET n.state = row.%v": true,
	} {
		if got := unmatchableDynamicRowKey(lit); got != want {
			t.Errorf("unmatchableDynamicRowKey(%q) = %v, want %v", lit, got, want)
		}
	}
}

// TestWritePathRowKeysMatchSource derives the row keys the write path reads
// from its Cypher literals and requires writePathRowKeys to equal the ones the
// snake_case rule does not already cover, so a new writer that reads
// row.<plainkey> under a different property name cannot fall outside the
// unresolved_row_tokens check (#6782 F-6). A stale entry fails too, so the
// vocabulary never widens past what a writer can actually store.
func TestWritePathRowKeysMatchSource(t *testing.T) {
	t.Parallel()

	derived := map[string]struct{}{}
	for path, lits := range writePathLiterals(t) {
		for _, lit := range lits {
			for _, m := range rowKeyRead.FindAllStringSubmatch(lit, -1) {
				if !strings.Contains(m[1], "_") {
					derived[m[1]] = struct{}{}
				}
			}
			if unmatchableDynamicRowKey(lit) {
				t.Errorf("%s builds a row key at run time that is not snake_case, so unresolved_row_tokens cannot match it:\n%s", path, lit)
			}
		}
	}
	var missing, stale []string
	for k := range derived {
		if _, ok := writePathRowKeys[k]; !ok {
			missing = append(missing, k)
		}
	}
	for k := range writePathRowKeys {
		if _, ok := derived[k]; !ok {
			stale = append(stale, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 || len(stale) > 0 {
		t.Fatalf("writePathRowKeys drifted from the write-path Cypher literals:\n  add:    %q\n  remove: %q", missing, stale)
	}
}

// readOnlyMapBindings are the UNWIND bindings other than row whose map fields
// a statement reads. Each entry must name a statement that never writes the
// fields into the graph, or the unresolved_row_tokens check cannot see them.
var readOnlyMapBindings = map[string]string{
	// reducer/code/value.CloudSinkTargetsByPairCypher: a MATCH-only loader.
	"pair": "reducer/code/value/cloud_sink_loader.go",
	// crossplaneSatisfiedByEdgeExistsCypher: MATCH ... RETURN existence probe.
	"candidate": "reducer/crossplane/crossplane_satisfied_by_edge_existence.go",
	// Orphan-sweep reads and marker writes: candidate_key.key_N only anchors
	// MATCH/WHERE; the writes store $observed_at_unix or REMOVE the marker.
	"candidate_key": "storage/cypher/orphan_sweep_queries.go, orphan_sweep_writes.go",
}

// mapDereferencedBindings returns the UNWIND bindings other than row that lit
// dereferences as maps (`<binding>.<field>`).
func mapDereferencedBindings(lit string) []string {
	var out []string
	for _, m := range unwindBinding.FindAllStringSubmatch(lit, -1) {
		binding := m[1]
		if binding == "row" {
			continue
		}
		deref := regexp.MustCompile(`\b` + regexp.QuoteMeta(binding) + `\.[A-Za-z_]`)
		if deref.MatchString(lit) {
			out = append(out, binding)
		}
	}
	return out
}

// TestMapDereferencedBindingsSeeded is the seeded RED/GREEN pair for the
// binding scan TestOnlyRowBindingWritesMapValues relies on.
func TestMapDereferencedBindingsSeeded(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		lit  string
		want string
	}{
		{lit: "UNWIND $rows AS r MERGE (n:X {uid: r.uid}) SET n.state = r.state", want: "r"},
		{lit: "UNWIND $rows AS row MERGE (n:X {uid: row.uid}) SET n.state = row.state", want: ""},
		{lit: "UNWIND $uids AS uid MATCH (n:X {uid: uid}) SET n.seen = true", want: ""},
		{lit: "unwind $pairs as pair MATCH (f:Function {uid: pair.function_uid})", want: "pair"},
		// UNWIND and dereference in separate literals, joined per file.
		{lit: "UNWIND $keys AS k\nMATCH (n:%s {%s: %s})\nk.key_0", want: "k"},
	} {
		if got := strings.Join(mapDereferencedBindings(tc.lit), ","); got != tc.want {
			t.Errorf("mapDereferencedBindings(%q) = %q, want %q", tc.lit, got, tc.want)
		}
	}
}

// TestOnlyRowBindingWritesMapValues pins the premise the unresolved_row_tokens
// check rests on: `row` is the only UNWIND binding whose map fields a graph
// write can store. Any other map-dereferenced binding must be a reviewed
// read-only entry in readOnlyMapBindings; a stale entry fails too.
func TestOnlyRowBindingWritesMapValues(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	for path, lits := range writePathLiterals(t) {
		for _, binding := range mapDereferencedBindings(strings.Join(lits, "\n")) {
			seen[binding] = true
			if _, ok := readOnlyMapBindings[binding]; !ok {
				t.Errorf("%s: UNWIND binding %q is dereferenced as a map; name it row so unresolved_row_tokens covers it, or list it in readOnlyMapBindings if no statement writes its fields", path, binding)
			}
		}
	}
	for binding, file := range readOnlyMapBindings {
		if !seen[binding] {
			t.Errorf("readOnlyMapBindings entry %q (%s) no longer appears in the write path; remove it", binding, file)
		}
	}
}
