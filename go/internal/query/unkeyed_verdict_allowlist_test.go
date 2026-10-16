// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// noCapabilityAllowedFiles lists the files, by path relative to
// go/internal/query, whose WriteGraphReadError and GraphReadErrorEnvelope
// call sites may pass querycontract.NoCapability instead of a
// capability-matrix key. Each entry names the routes there that read through
// the guarded PostgreSQL reader (or the graph) with no matrix row, and #8227,
// which registers their keys and deletes the entry. A key matches one file
// exactly: never a directory, a subdirectory, or a same-named file elsewhere.
// A bare "" literal is never accepted, and an entry whose file has no
// NoCapability call site fails as stale.
var noCapabilityAllowedFiles = map[string]string{
	"status.go": "GET /api/v0/status/pipeline, GET /api/v0/status/collectors, " +
		"GET /api/v0/collectors, GET /api/v0/status/ingesters/{ingester}, " +
		"GET /api/v0/ingesters/{ingester}, and the unscoped GET /api/v0/status/index " +
		"and GET /api/v0/index-status read the status snapshot with no matrix key (#8227)",
	"status_scoped.go": "the scoped GET /api/v0/status/index and GET /api/v0/index-status " +
		"graph repository count has no matrix key (#8227)",
	"status_hosted_readiness.go": "GET /api/v0/status/hosted-readiness reads the status " +
		"snapshot with no matrix key (#8227)",
	"status_freshness_causality.go": "GET /api/v0/status/freshness-causality reads the " +
		"status snapshot with no matrix key (#8227)",
	"status_collector_readiness.go": "GET /api/v0/status/collector-readiness and GET " +
		"/api/v0/collector-readiness read the status snapshot with no matrix key (#8227)",
	"evidence_bundle_live.go": "GET /api/v0/evidence/bundle reads the status snapshot " +
		"with no matrix key (#8227)",
	"admin/identity/audit.go": "GET /api/v0/auth/admin/audit/events and GET " +
		"/api/v0/auth/admin/audit/summary read the governance audit store with no " +
		"matrix key (#8227)",
}

// noCapabilityAllowedFileCount pins how many files may pass
// querycontract.NoCapability, so adding or dropping one is a visible,
// reviewed change to this constant as well as to the map (#8227 removes them).
const noCapabilityAllowedFileCount = 7

// noCapabilityConstName is the querycontract constant the sweep recognizes.
const noCapabilityConstName = "NoCapability"

// queryRootMarker locates go/internal/query in a swept path, so an allowlist
// key is matched against the path relative to that root.
const queryRootMarker = "/internal/query/"

// isNoCapabilityArg reports whether expr names querycontract.NoCapability:
// a selector on an identifier the consuming file imports as the
// querycontract package. A same-spelled selector on any other package or
// value is not it.
func (s *capabilitySweep) isNoCapabilityArg(expr ast.Expr) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok || sel.Sel == nil || sel.Sel.Name != noCapabilityConstName {
		return false
	}
	pkgIdent, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	importPath, ok := s.fileImports[s.fset.Position(sel.Pos()).Filename][pkgIdent.Name]
	return ok && path.Base(importPath) == "querycontract"
}

// queryRelPath returns filename relative to go/internal/query, or "" when it
// is not under that root.
func queryRelPath(filename string) string {
	slashed := filepath.ToSlash(filename)
	i := strings.LastIndex(slashed, queryRootMarker)
	if i < 0 {
		return ""
	}
	return slashed[i+len(queryRootMarker):]
}

// noCapabilityFindings returns no finding for a querycontract.NoCapability
// call site in an allowlisted file, and one finding anywhere else.
func (s *capabilitySweep) noCapabilityFindings(pos token.Pos) []string {
	position := s.fset.Position(pos)
	if _, ok := noCapabilityAllowedFiles[queryRelPath(position.Filename)]; ok {
		return nil
	}
	return []string{"querycontract.NoCapability at " + position.String() +
		" is outside noCapabilityAllowedFiles; pass a capability-matrix key (#8227)"}
}

// noCapabilitySiteFiles returns the go/internal/query-relative path of every
// file in files that holds at least one swept call passing
// querycontract.NoCapability.
func (s *capabilitySweep) noCapabilitySiteFiles(files []*ast.File) map[string]bool {
	seen := map[string]bool{}
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			capIdx, swept := callsWriteGraphReadError(call.Fun)
			if swept && len(call.Args) > capIdx && s.isNoCapabilityArg(call.Args[capIdx]) {
				seen[queryRelPath(s.fset.Position(call.Pos()).Filename)] = true
			}
			return true
		})
	}
	return seen
}

// staleNoCapabilityEntries returns one finding per allowed entry whose file
// holds no NoCapability call site: the routes there gained a key, or moved,
// and the entry must go.
func staleNoCapabilityEntries(allowed map[string]string, seen map[string]bool) []string {
	var findings []string
	for rel := range allowed {
		if !seen[rel] {
			findings = append(findings, "noCapabilityAllowedFiles entry "+rel+
				" has no querycontract.NoCapability call site; delete the stale entry (#8227)")
		}
	}
	sort.Strings(findings)
	return findings
}

// TestNoCapabilityAllowlistHasThePinnedSize fails when the allowlist grows or
// shrinks without noCapabilityAllowedFileCount moving with it.
func TestNoCapabilityAllowlistHasThePinnedSize(t *testing.T) {
	t.Parallel()

	if got := len(noCapabilityAllowedFiles); got != noCapabilityAllowedFileCount {
		t.Fatalf("noCapabilityAllowedFiles has %d entries, want %d (noCapabilityAllowedFileCount)", got, noCapabilityAllowedFileCount)
	}
}

// TestCapabilitySweepNoCapabilityIsAllowlistedByFile is the seeded RED/GREEN
// set for the NoCapability form. On-disk files under a fake internal/query
// tree, swept by the real capabilitySweep, prove an allowlisted file may pass
// querycontract.NoCapability; a non-allowlisted file in the same directory,
// and a same-named file in a subdirectory, may not; a bare "" literal fails
// even inside an allowlisted file; and an allowlisted file with no
// NoCapability call site is reported stale.
func TestCapabilitySweepNoCapabilityIsAllowlistedByFile(t *testing.T) {
	t.Parallel()

	allowedNames := make([]string, 0, len(noCapabilityAllowedFiles))
	for rel := range noCapabilityAllowedFiles {
		allowedNames = append(allowedNames, rel)
	}
	sort.Strings(allowedNames)
	if len(allowedNames) == 0 {
		t.Skip("noCapabilityAllowedFiles is empty: #8227 registered every key")
	}
	allowed := allowedNames[0]

	root := filepath.Join(t.TempDir(), "internal", "query")
	write := func(rel, src string) string {
		t.Helper()
		file := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(file), err)
		}
		if err := os.WriteFile(file, []byte(src), 0o600); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
		return file
	}
	const contract = "package querycontract\n\nconst NoCapability = \"\"\n\n" +
		"func WriteGraphReadError(w, r, err any, capability string) bool { return false }\n"
	route := func(pkg, arg string) string {
		return "package " + pkg + "\n\nimport \"example.test/internal/query/querycontract\"\n\n" +
			"func handle(w, r, err any) { querycontract.WriteGraphReadError(w, r, err, " + arg + ") }\n"
	}

	fset := token.NewFileSet()
	sweep := newCapabilitySweep(fset)
	parse := func(name string) *ast.File {
		t.Helper()
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		sweep.collectDecls(file)
		return file
	}
	parse(write("querycontract/contract.go", contract))

	cases := []struct {
		name        string
		rel         string
		arg         string
		wantFinding bool
	}{
		{name: "allowlisted file", rel: allowed, arg: "querycontract.NoCapability"},
		{name: "same directory, not allowlisted", rel: "unlisted_route.go", arg: "querycontract.NoCapability", wantFinding: true},
		{name: "same name in a subdirectory", rel: "sub/" + allowed, arg: "querycontract.NoCapability", wantFinding: true},
		{name: `bare "" in the allowlisted file`, rel: allowed, arg: `""`, wantFinding: true},
	}
	for _, tc := range cases {
		file := parse(write(tc.rel, route("query", tc.arg)))
		findings, matched := sweep.findCallSites(file)
		if matched != 1 {
			t.Fatalf("%s: matched %d call sites, want 1", tc.name, matched)
		}
		if got := len(findings) > 0; got != tc.wantFinding {
			t.Errorf("%s: finding = %v (%v), want %v", tc.name, got, findings, tc.wantFinding)
		}
	}

	// The allowlisted file now holds the bare "" form only, so its entry is
	// stale; the subdirectory file with the same name does not count for it.
	stale := parse(write(allowed, route("query", `""`)))
	sub := parse(filepath.Join(root, "sub", filepath.FromSlash(allowed)))
	seen := sweep.noCapabilitySiteFiles([]*ast.File{stale, sub})
	findings := staleNoCapabilityEntries(map[string]string{allowed: "seeded"}, seen)
	if len(findings) != 1 {
		t.Errorf("stale entry: findings = %v, want exactly one for %s", findings, allowed)
	}
	live := parse(write(allowed, route("query", "querycontract.NoCapability")))
	seen = sweep.noCapabilitySiteFiles([]*ast.File{live})
	if findings := staleNoCapabilityEntries(map[string]string{allowed: "seeded"}, seen); len(findings) != 0 {
		t.Errorf("live entry: findings = %v, want none", findings)
	}
}
