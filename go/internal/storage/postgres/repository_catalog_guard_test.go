// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// repositoryCatalogOmittedFields are the decoded codegraph Repository fields
// that carry the projected-away payload keys. A consumer reading one of them
// off a catalog envelope would silently see an empty value.
var repositoryCatalogOmittedFields = map[string]string{
	"GitRefs":                   "git_refs",
	"DeltaRelativePaths":        "delta_relative_paths",
	"DeltaDeletedRelativePaths": "delta_deleted_relative_paths",
	"ImportsMap":                "imports_map",
}

// repositoryCatalogSharedHelperDirs are the two reducer helper packages that
// catalog consumers read repository payloads through. The guard parses them
// along with each consumer's domain directory; it parses no other package.
var repositoryCatalogSharedHelperDirs = []string{
	"internal/reducer/payloadcore",
	"internal/reducer/factload",
}

// TestRepositoryCatalogConsumersReadNoProjectedKey is the #9567 guard. It
// discovers every non-test caller of ListActiveRepositoryFacts under go/,
// widens each reducer caller to its whole domain directory, and fails if a
// non-test file in those directories, or in payloadcore or factload, names a
// payload key or decoded field the catalog read projects away.
//
// The scan is syntactic and covers direct reads only. It does not follow
// calls into other packages, so a consumer that passes catalog envelopes to
// a helper elsewhere (for example facts.ParseDeltaScope, which reads the
// delta keys) passes this guard. No consumer does that today: the only
// package in the consumers' dependency sets that reads a dropped key is
// internal/facts, and no consumer calls its delta helpers on catalog
// envelopes (#9567 review). A consumer that needs a dropped key must get a
// separate, non-projected store method instead.
func TestRepositoryCatalogConsumersReadNoProjectedKey(t *testing.T) {
	t.Parallel()

	goRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve go root: %v", err)
	}
	callers := repositoryCatalogCallerDirs(t, goRoot)
	for _, want := range []string{
		"internal/reducer/containerimage",
		"internal/reducer/packages/correlation",
		"internal/reducer/securityalert",
		"internal/reducer/servicecatalog",
		"internal/reducer/supplychain/core",
	} {
		if _, ok := callers[want]; !ok {
			t.Fatalf("caller discovery missed %s (found %v); the guard would pass vacuously", want, repositoryCatalogSortedKeys(callers))
		}
	}

	scan := map[string]struct{}{}
	for dir := range callers {
		scan[repositoryCatalogScanRoot(dir)] = struct{}{}
	}
	for _, dir := range repositoryCatalogSharedHelperDirs {
		scan[dir] = struct{}{}
	}
	var violations []string
	for _, dir := range repositoryCatalogSortedKeys(scan) {
		violations = append(violations, repositoryCatalogViolations(t, filepath.Join(goRoot, dir))...)
	}
	if len(violations) > 0 {
		t.Fatalf("repository catalog consumers read keys the #9567 projection drops; give them a separate non-projected method:\n%s",
			strings.Join(violations, "\n"))
	}
}

// TestRepositoryCatalogGuardFlagsPlantedViolation is the seeded RED/GREEN pair
// for the guard: the planted consumer reads imports_map and GitRefs and must
// be flagged twice; the clean consumer reads identity keys only.
func TestRepositoryCatalogGuardFlagsPlantedViolation(t *testing.T) {
	t.Parallel()

	red := repositoryCatalogViolations(t, filepath.Join("testdata", "repositorycatalogguard", "violation"))
	if len(red) != 2 || !strings.Contains(red[0]+red[1], `"imports_map"`) || !strings.Contains(red[0]+red[1], "GitRefs") {
		t.Fatalf("planted violation not flagged as imports_map + GitRefs, got %v", red)
	}
	if green := repositoryCatalogViolations(t, filepath.Join("testdata", "repositorycatalogguard", "clean")); len(green) != 0 {
		t.Fatalf("clean consumer flagged: %v", green)
	}
}

// repositoryCatalogCallerDirs returns the go/-relative directories of every
// non-test file that calls ListActiveRepositoryFacts, excluding this store.
func repositoryCatalogCallerDirs(t *testing.T, goRoot string) map[string]struct{} {
	t.Helper()
	callers := map[string]struct{}{}
	err := filepath.WalkDir(goRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case "testdata", "vendor", ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(string(raw), ".ListActiveRepositoryFacts(") {
			return nil
		}
		rel, err := filepath.Rel(goRoot, filepath.Dir(path))
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel != "internal/storage/postgres" {
			callers[rel] = struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", goRoot, err)
	}
	return callers
}

// repositoryCatalogScanRoot widens a reducer caller to its domain directory
// (internal/reducer/<domain>) so sibling packages that receive the envelopes,
// such as packages/source, are scanned too. Other callers scan their own dir.
func repositoryCatalogScanRoot(dir string) string {
	const prefix = "internal/reducer/"
	if !strings.HasPrefix(dir, prefix) {
		return dir
	}
	domain, _, _ := strings.Cut(strings.TrimPrefix(dir, prefix), "/")
	return prefix + domain
}

// repositoryCatalogViolations parses every non-test .go file under root and
// reports string literals naming an omitted payload key and selectors naming
// an omitted decoded field.
func repositoryCatalogViolations(t *testing.T, root string) []string {
	t.Helper()
	omitted := map[string]struct{}{}
	for _, key := range facts.RepositoryCatalogOmittedPayloadKeys() {
		omitted[key] = struct{}{}
	}
	var out []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.BasicLit:
				if n.Kind != token.STRING {
					return true
				}
				if value, err := strconv.Unquote(n.Value); err == nil {
					if _, ok := omitted[value]; ok {
						out = append(out, fset.Position(n.Pos()).String()+": reads payload key "+n.Value)
					}
				}
			case *ast.SelectorExpr:
				if key, ok := repositoryCatalogOmittedFields[n.Sel.Name]; ok {
					out = append(out, fset.Position(n.Pos()).String()+": reads decoded field "+n.Sel.Name+" ("+key+")")
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	return out
}

func repositoryCatalogSortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
