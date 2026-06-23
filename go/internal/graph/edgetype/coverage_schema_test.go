package edgetype

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// relPattern matches a Cypher relationship pattern's type list inside a string
// literal: an arrow head `-[` or `<-[` followed by an optional variable, a
// colon, and one or more ALL_CAPS_SNAKE relationship types joined by `|`
// (the alternation form used in retract statements such as
// `-[rel:DEPLOYS_FROM|USES_MODULE]->`). The captured group is the type list;
// individual types are extracted with typeToken.
var relPattern = regexp.MustCompile(
	`<?-\[\s*(?:[A-Za-z_][A-Za-z0-9_]*\s*)?:\s*([A-Z][A-Za-z0-9_]*(?:\s*\|\s*[A-Z][A-Za-z0-9_]*)*)`,
)

// typeToken extracts one relationship-type identifier from a type list.
var typeToken = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]*`)

// dynamicEdgePrefixes names the data-driven cloud relationship families that
// are synthesized from collector row data at runtime ("AWS_"+raw, "GCP_"+raw in
// the cloud resource edge writers). They are open sets that cannot be
// enumerated as constants, so the registry deliberately excludes them and the
// coverage test skips any literal carrying these prefixes.
var dynamicEdgePrefixes = []string{"AWS_", "GCP_"}

// TestNoUnregisteredEdgeLiteral scans every production (non-test) Go source file
// in the module for Cypher relationship-type literals and asserts that each is a
// registered EdgeType. This is the schema test required by issue #3491: it fails
// CI the moment a new `-[:NEW_TYPE]->` literal is introduced without adding
// NEW_TYPE to the registry, so no edge type can live outside this package.
func TestNoUnregisteredEdgeLiteral(t *testing.T) {
	root := moduleRoot(t)

	type finding struct {
		edge string
		file string
	}
	var unregistered []finding

	fset := token.NewFileSet()
	walkErr := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "vendor", "testdata", ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			// A parse failure here is unrelated to edge coverage; skip rather
			// than fail the whole gate on an unrelated syntax issue.
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			val, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			for _, edge := range edgeTypesIn(val) {
				if !IsRegistered(edge) {
					unregistered = append(unregistered, finding{edge: edge, file: rel})
				}
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking module root %q: %v", root, walkErr)
	}

	if len(unregistered) > 0 {
		sort.Slice(unregistered, func(i, j int) bool {
			if unregistered[i].edge != unregistered[j].edge {
				return unregistered[i].edge < unregistered[j].edge
			}
			return unregistered[i].file < unregistered[j].file
		})
		for _, u := range unregistered {
			t.Errorf("unregistered edge type %q in %s: add it to internal/graph/edgetype", u.edge, u.file)
		}
		t.Fatalf("%d unregistered edge-type literal(s) found; every graph relationship type must be registered", len(unregistered))
	}
}

// edgeTypesIn extracts the registry-relevant relationship types from one string
// literal value, dropping data-driven dynamic families.
func edgeTypesIn(val string) []string {
	var out []string
	for _, m := range relPattern.FindAllStringSubmatch(val, -1) {
		for _, tok := range typeToken.FindAllString(m[1], -1) {
			if skipDynamic(tok) {
				continue
			}
			out = append(out, tok)
		}
	}
	return out
}

// skipDynamic reports whether a captured token belongs to a runtime data-driven
// edge family rather than a statically registrable type. This covers the
// "AWS_"/"GCP_" cloud prefixes (whose suffixes are lowercase, collector-derived)
// and any token that is not pure ALL_CAPS_SNAKE (e.g. the lowercase tail of an
// "AWS_acm_certificate_used_by_resource" read pattern).
func skipDynamic(tok string) bool {
	for _, p := range dynamicEdgePrefixes {
		if strings.HasPrefix(tok, p) {
			return true
		}
	}
	for _, r := range tok {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		// Contains a lowercase letter: a data-driven/dynamic type, not a
		// statically named graph edge type.
		return true
	}
	return false
}

// moduleRoot walks upward from this test file until it finds the directory that
// contains go.mod, returning the Go module root so the scan covers the whole
// module regardless of where `go test` is invoked.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed; cannot locate module root")
	}
	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("reached filesystem root without finding go.mod")
		}
		dir = parent
	}
}
