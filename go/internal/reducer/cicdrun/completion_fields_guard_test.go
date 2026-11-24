// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cicdrun

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The completion change gate (#10972 PR-4) suppresses the ci_cd completion
// event when no field in completionDigestFields changed. That is only safe
// while every reducer read of a reducer_ci_cd_run_correlation payload reads a
// field in that list. This guard scans the reducer and fact-index source for
// those reads, so a new read of an unhashed field fails here, not as a silently
// missed supply_chain_impact refresh. Query-time readers (go/internal/query)
// read live rows per request and wait for no completion event, so they are out
// of scope.
//
// Limits: the scan sees only the read shapes listed on scanCICDPayloadReads.
// It does not see:
//   - a read gated by an if (envelope.FactKind == cicdrun.CICDRunCorrelationFactKind)
//     instead of a switch case;
//   - a key passed as a named constant or variable instead of a string literal;
//   - a read through a method, or through a helper in another package whose
//     name does not contain "payload";
//   - a range over envelope.Payload, or over a copy of the map;
//   - the kind spelled as the string literal "reducer_ci_cd_run_correlation"
//     instead of an identifier containing CICDRunCorrelation.
//
// A new reader written in one of these shapes passes silently, so a new reader
// shape needs the guard extended in the same change.

// completionGuardRoots are the source trees that hold the completion
// consumer's reads, relative to this package directory.
var completionGuardRoots = []string{"..", "../../storage/postgres/facts"}

// cicdKindMarker matches the ci_cd fact-kind identifiers: the writer constant
// cicdrun.CICDRunCorrelationFactKind and the index constant KindCICDRunCorrelation.
const cicdKindMarker = "CICDRunCorrelation"

// payloadKeyRead is one payload key a reader reads for the ci_cd kind.
type payloadKeyRead struct {
	file string
	key  string
}

// scanCICDPayloadReads parses every non-test Go file under root, outside the
// cicdrun writer package, that names the ci_cd kind, and returns the payload
// keys read for it:
//   - in a switch case listing the kind, every string-literal argument of a
//     payload accessor call (a callee whose name contains "payload") and every
//     Payload["key"] index, following calls to functions declared in the same
//     package directory;
//   - in a keyProbe(name, anchor, key, param, kinds...) call whose kinds name
//     the ci_cd kind, the key argument.
func scanCICDPayloadReads(t *testing.T, root string) []payloadKeyRead {
	t.Helper()
	packages := map[string][]*ast.File{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if entry.Name() == "cicdrun" || entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		dir := filepath.Dir(path)
		packages[dir] = append(packages[dir], file)
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	var reads []payloadKeyRead
	for _, files := range packages {
		funcs := map[string]*ast.FuncDecl{}
		for _, file := range files {
			for _, decl := range file.Decls {
				if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Body != nil {
					funcs[fn.Name.Name] = fn
				}
			}
		}
		for _, file := range files {
			name := filepath.Base(fset.Position(file.Pos()).Filename)
			ast.Inspect(file, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.CaseClause:
					if namesCICDKind(n.List, funcs) {
						visited := map[string]bool{}
						for _, stmt := range n.Body {
							reads = append(reads, payloadKeysIn(fset, stmt, funcs, visited)...)
						}
					}
				case *ast.CallExpr:
					if callName(n.Fun) == "keyProbe" && len(n.Args) >= 5 && namesCICDKind(n.Args[4:], funcs) {
						if key, ok := stringLiteral(n.Args[2]); ok {
							reads = append(reads, payloadKeyRead{file: name, key: key})
						}
					}
				}
				return true
			})
		}
	}
	return reads
}

// payloadKeysIn returns the payload keys read under node, each credited to
// the file that holds the read, following calls to package functions once
// each.
func payloadKeysIn(fset *token.FileSet, node ast.Node, funcs map[string]*ast.FuncDecl, visited map[string]bool) []payloadKeyRead {
	var keys []payloadKeyRead
	at := func(pos token.Pos, key string) payloadKeyRead {
		return payloadKeyRead{file: filepath.Base(fset.Position(pos).Filename), key: key}
	}
	ast.Inspect(node, func(inner ast.Node) bool {
		switch n := inner.(type) {
		case *ast.CallExpr:
			name := callName(n.Fun)
			if strings.Contains(strings.ToLower(name), "payload") {
				for _, arg := range n.Args {
					if key, ok := stringLiteral(arg); ok {
						keys = append(keys, at(arg.Pos(), key))
					}
				}
			}
			if fn, ok := funcs[name]; ok && !visited[name] {
				visited[name] = true
				keys = append(keys, payloadKeysIn(fset, fn.Body, funcs, visited)...)
			}
		case *ast.IndexExpr:
			if sel, ok := n.X.(*ast.SelectorExpr); ok && sel.Sel.Name == "Payload" {
				if key, ok := stringLiteral(n.Index); ok {
					keys = append(keys, at(n.Index.Pos(), key))
				}
			}
		}
		return true
	})
	return keys
}

// namesCICDKind reports whether exprs name the ci_cd kind directly or through
// a package function that returns a kind list (repositoryGrowthKinds()...).
func namesCICDKind(exprs []ast.Expr, funcs map[string]*ast.FuncDecl) bool {
	for _, expr := range exprs {
		if strings.Contains(callName(expr), cicdKindMarker) {
			return true
		}
		call, ok := expr.(*ast.CallExpr)
		if !ok {
			continue
		}
		fn, ok := funcs[callName(call.Fun)]
		if !ok {
			continue
		}
		found := false
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok && strings.Contains(ident.Name, cicdKindMarker) {
				found = true
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// callName returns the identifier or selector name of expr, or "".
func callName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.SelectorExpr:
		return e.Sel.Name
	}
	return ""
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	return value, err == nil
}

// completionIdentityFields are the payload keys the fact id is derived from
// (cicdRunCorrelationIdentity). A read of one is covered without the digest:
// its value cannot change without a new fact id, which the verdict counts as
// changed. TestCompletionIdentityFieldsBindTheFactID proves each one does.
func completionIdentityFields() []string {
	identity := cicdRunCorrelationIdentity(CICDRunCorrelationWrite{}, CICDRunCorrelationDecision{})
	keys := make([]string, 0, len(identity))
	for key := range identity {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// unhashedReads returns the reads of keys that are neither in
// completionDigestFields nor fact-id identity fields.
func unhashedReads(reads []payloadKeyRead) []string {
	identity := completionIdentityFields()
	var missing []string
	for _, read := range reads {
		if !slices.Contains(completionDigestFields, read.key) && !slices.Contains(identity, read.key) {
			missing = append(missing, read.file+": "+read.key)
		}
	}
	sort.Strings(missing)
	return missing
}

// TestCompletionDigestCoversEveryConsumerPayloadRead is the guard: every
// reducer and fact-index read of a ci_cd correlation payload reads a hashed
// field. It also fails if the scan finds no read in a file known to read one,
// so a scanner that silently matches nothing cannot pass.
func TestCompletionDigestCoversEveryConsumerPayloadRead(t *testing.T) {
	t.Parallel()

	var reads []payloadKeyRead
	for _, root := range completionGuardRoots {
		reads = append(reads, scanCICDPayloadReads(t, root)...)
	}
	seen := map[string]bool{}
	for _, read := range reads {
		seen[read.file+": "+read.key] = true
	}
	t.Logf("ci_cd payload reads: %d distinct (%v)", len(seen), sortedKeys(seen))
	if missing := unhashedReads(reads); len(missing) > 0 {
		t.Fatalf("ci_cd payload reads outside completionDigestFields: %v; add the field to the digest or the gate suppresses refreshes this reader needs", missing)
	}
	for _, file := range []string{"deployment_context.go", "active_filter.go", "criteria_bound.go", "security_alert_scope.go", "branches.go"} {
		if !slices.ContainsFunc(reads, func(read payloadKeyRead) bool { return read.file == file }) {
			t.Fatalf("scan found no ci_cd payload read in %s; the scanner no longer sees that reader", file)
		}
	}
}

// TestCompletionDigestGuardCatchesPlantedReads is the seeded RED/GREEN pair:
// planted readers of unhashed keys (commit_sha, cause, source_system) through
// a case clause, a followed decoder call, a Payload index, a keyProbe naming
// the kind and a keyProbe over a kind-list function are each reported; a
// keyProbe over a list without the kind is not a read; the same shapes
// reading a hashed key or an identity key are allowed.
func TestCompletionDigestGuardCatchesPlantedReads(t *testing.T) {
	t.Parallel()

	planted := func(key string) string {
		return `package planted

func decode(envelope facts.Envelope) string { return payloadcore.PayloadStr(envelope.Payload, "` + key + `") }

func read(envelope facts.Envelope) {
	switch envelope.FactKind {
	case cicdrun.CICDRunCorrelationFactKind:
		_ = decode(envelope)
		_ = payloadBool(envelope.Payload, "` + key + `")
		_ = envelope.Payload["` + key + `"]
	}
}

var probes = []Branch{
	keyProbe("p", IndexX, "` + key + `", Param, KindOther, KindCICDRunCorrelation),
	keyProbe("q", IndexY, "` + key + `", Param, growthKinds()...),
	keyProbe("r", IndexZ, "` + key + `", Param, otherKinds()...),
}

func growthKinds() []string { return []string{KindOther, KindCICDRunCorrelation} }

func otherKinds() []string { return []string{KindOther} }
`
	}
	for _, tc := range []struct {
		key  string
		want int
	}{{"commit_sha", 5}, {"cause", 5}, {"source_system", 5}, {"artifact_digest", 0}, {"run_id", 0}} {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "planted.go"), []byte(planted(tc.key)), 0o600); err != nil {
			t.Fatal(err)
		}
		reads := scanCICDPayloadReads(t, root)
		if len(reads) != 5 {
			t.Fatalf("%s: scanner saw %d planted reads, want 5: %v", tc.key, len(reads), reads)
		}
		if got := len(unhashedReads(reads)); got != tc.want {
			t.Fatalf("%s: guard reported %d unhashed reads, want %d", tc.key, got, tc.want)
		}
	}
}

// TestCompletionIdentityFieldsBindTheFactID proves the identity allowance:
// changing any identity field gives the row a new fact id, which
// completionChanged counts as changed.
func TestCompletionIdentityFieldsBindTheFactID(t *testing.T) {
	t.Parallel()

	write := CICDRunCorrelationWrite{ScopeID: "scope", GenerationID: "generation"}
	decision := CICDRunCorrelationDecision{Provider: "github_actions", RunID: "1", RunAttempt: "1"}
	base := cicdRunCorrelationFactID(write, decision)
	mutations := map[string]func(*CICDRunCorrelationWrite, *CICDRunCorrelationDecision){
		"scope_id":      func(w *CICDRunCorrelationWrite, _ *CICDRunCorrelationDecision) { w.ScopeID = "other" },
		"generation_id": func(w *CICDRunCorrelationWrite, _ *CICDRunCorrelationDecision) { w.GenerationID = "other" },
		"provider":      func(_ *CICDRunCorrelationWrite, d *CICDRunCorrelationDecision) { d.Provider = "other" },
		"run_id":        func(_ *CICDRunCorrelationWrite, d *CICDRunCorrelationDecision) { d.RunID = "other" },
		"run_attempt":   func(_ *CICDRunCorrelationWrite, d *CICDRunCorrelationDecision) { d.RunAttempt = "other" },
	}
	for _, key := range completionIdentityFields() {
		mutate, ok := mutations[key]
		if !ok {
			t.Fatalf("identity field %q has no fact-id proof here; add one before the guard allows reads of it", key)
		}
		w, d := write, decision
		mutate(&w, &d)
		if cicdRunCorrelationFactID(w, d) == base {
			t.Fatalf("changing identity field %q kept the fact id; a read of it is not covered", key)
		}
	}
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
