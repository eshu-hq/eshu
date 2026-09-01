// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// KnownDecodeQualifiers is matched against the qualifier IDENTIFIER as written
// at the call site, not against the resolved import path. Nothing enforced that
// invariant: an aliased import (`import fs ".../factschema"`) or a brand-new
// qualified decode surface would make decodeCallName stop recognizing those
// call sites, and the manifest would quietly shrink -- the same
// silent-undercount this package exists to close, relocated onto the allowlist.
//
// The two tests below make that drift fail loudly instead.

// decodeSurfaceDirs returns the six trees Load feeds to ScanDecodeUsage, taken
// from ResolvePaths rather than written out here.
//
// The first version of this helper hard-coded the six paths and got two of them
// wrong: "go/internal/loader", which does not exist (LoaderDir actually resolves
// to go/internal/storage/postgres), and "go/internal/replay", a superset of the
// real go/internal/replay/offlinetier. Because walkParsedGoFiles skips a missing
// directory, the wrong one contributed zero files and nothing said so -- and the
// surface it silently dropped is the one KnownDecodeQualifiers' own doc comment
// cites as the reason "factschema" is on the allowlist at all. The three
// mutations this file is meant to catch were still caught, but only because
// those qualifiers happen to be used in the correctly-named trees too. A guard
// that reads a path nobody maintains is not guarding the thing it names.
func decodeSurfaceDirs(t *testing.T) []string {
	t.Helper()

	resolved := ResolvePaths(Paths{RepoRoot: repoRoot(t)})
	return []string{
		resolved.ReducerDir,
		resolved.ProjectorDir,
		resolved.QueryDir,
		resolved.LoaderDir,
		resolved.RelationshipsDir,
		resolved.ReplayDir,
	}
}

// requireNonEmptySurfaces fails when any surface contributed zero parsed files.
//
// walkParsedGoFiles tolerates a missing directory on purpose, so that a tree
// relocated by the in-flight #6061 restructure does not turn this guard red for
// a reason that has nothing to do with qualifier drift. That tolerance is also
// how a wrong path hides, so the count per surface is asserted here instead:
// tolerate a moved tree, never tolerate a silently unscanned one.
func requireNonEmptySurfaces(t *testing.T, parsedPerDir map[string]int) {
	t.Helper()

	for dir, parsed := range parsedPerDir {
		if parsed == 0 {
			t.Errorf("decode surface %s contributed zero parsed Go files: this guard is not reading it, so any qualifier or alias drift there is invisible. Check the path against ResolvePaths in paths.go.", dir)
		}
	}
}

// walkParsedGoFiles parses every non-test .go file under dir and hands each one
// to visit, returning how many files it parsed so a caller can prove the walk
// was not empty. A missing directory is skipped rather than fatal: these trees
// move between packages during the #6061 restructure, and hard-failing on a
// relocated tree would be noise rather than signal.
func walkParsedGoFiles(t *testing.T, dir string, visit func(path string, file *ast.File)) int {
	t.Helper()

	fset := token.NewFileSet()
	parsed := 0
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			return nil
		}
		parsed++
		visit(path, file)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return parsed
}

// isDecodeSeamCallName reports whether name has the Decode<Word> shape a decode
// seam uses, rejecting a bare "Decode" and a lowercase continuation so an
// unrelated json.Decoder-style method cannot be mistaken for one.
func isDecodeSeamCallName(name string) bool {
	const prefix = "Decode"
	if !strings.HasPrefix(name, prefix) || len(name) == len(prefix) {
		return false
	}
	return unicode.IsUpper(rune(name[len(prefix)]))
}

// TestNoDecodeSeamIsReachedThroughAnUnknownQualifier fails when a real decode
// seam is called through a package qualifier KnownDecodeQualifiers omits.
//
// It deliberately does NOT assert that every `x.DecodeFoo(...)` call uses a
// known qualifier. Unrelated packages legitimately expose Decode-prefixed
// methods, so that assertion would be a false alarm rather than a guard. The
// real invariant is narrower: when a call names a function that IS one of the
// parsed seams, the qualifier it is reached through must be recognized --
// otherwise decodeCallName drops the call and its field reads disappear from
// the manifest with nothing turning red.
func TestNoDecodeSeamIsReachedThroughAnUnknownQualifier(t *testing.T) {
	t.Parallel()

	// Resolve the seam files the way Load does, rather than re-deriving a glob
	// here: a hand-written pattern that stops matching is exactly the vacuous
	// pass this guard exists to prevent, and it already caught one -- a "**"
	// pattern that resolved to zero seams.
	resolved := ResolvePaths(Paths{RepoRoot: repoRoot(t)})
	decodeFiles, err := resolveDecodeFiles(resolved)
	if err != nil {
		t.Fatalf("resolveDecodeFiles() error = %v", err)
	}

	var seams []DecodeSeam
	for _, decodeFile := range decodeFiles {
		parsedSeams, parseErr := ParseDecodeSeams(decodeFile)
		if parseErr != nil {
			t.Fatalf("ParseDecodeSeams(%s) error = %v", decodeFile, parseErr)
		}
		seams = append(seams, parsedSeams...)
	}

	seamNames := make(map[string]struct{}, len(seams))
	for _, seam := range seams {
		seamNames[seam.FuncName] = struct{}{}
	}
	if len(seamNames) == 0 {
		t.Fatal("parsed zero decode seams: this guard would pass vacuously, so the seam glob no longer resolves")
	}

	dirs := decodeSurfaceDirs(t)
	if len(dirs) != 6 {
		t.Fatalf("decodeSurfaceDirs() = %d dirs, want 6: Load feeds six trees to ScanDecodeUsage, so add the new one here too", len(dirs))
	}

	parsedPerDir := make(map[string]int, len(dirs))
	inspected := 0
	for _, dir := range dirs {
		parsed := walkParsedGoFiles(t, dir, func(path string, file *ast.File) {
			ast.Inspect(file, func(node ast.Node) bool {
				// Any SelectorExpr, not only a CallExpr: schemadecode seams are
				// reached as function VALUES in decode_seam_compat*.go
				// (`var decodeX = schemadecode.DecodeX`), 97 of them, and a
				// call-only walk cannot see a single one. Measured: with the
				// call-only form, deleting "schemadecode" from
				// KnownDecodeQualifiers left this test green.
				selector, isSelector := node.(*ast.SelectorExpr)
				if !isSelector {
					return true
				}
				qualifier, isIdent := selector.X.(*ast.Ident)
				if !isIdent || !isDecodeSeamCallName(selector.Sel.Name) {
					return true
				}
				if _, isSeam := seamNames[selector.Sel.Name]; !isSeam {
					return true
				}
				if _, known := KnownDecodeQualifiers[qualifier.Name]; !known {
					t.Errorf("%s calls decode seam %s through qualifier %q, which KnownDecodeQualifiers does not list: decodeCallName drops this call, so its field reads are missing from the payload-usage manifest with no failure anywhere. Add %q to KnownDecodeQualifiers, or route the call through a decode package already listed there.",
						path, selector.Sel.Name, qualifier.Name, qualifier.Name)
				}
				return true
			})
		})
		parsedPerDir[dir] = parsed
		inspected += parsed
	}
	requireNonEmptySurfaces(t, parsedPerDir)
	if inspected == 0 {
		t.Fatal("inspected zero Go files across all six decode surfaces: this guard would pass vacuously, so the surface paths are stale")
	}
}

// TestDecodePackagesAreImportedWithoutAnAlias fails when a scanned file imports
// factschema or schemadecode under an alias.
//
// KnownDecodeQualifiers holds bare package names, so an aliased import makes
// every qualified decode call in that file unrecognizable. The failure is
// invisible without this test: the manifest simply carries fewer field reads,
// and the schema gate consuming it stays green while covering less.
func TestDecodePackagesAreImportedWithoutAnAlias(t *testing.T) {
	t.Parallel()

	wantIdentifier := map[string]string{
		"github.com/eshu-hq/eshu/sdk/go/factschema":                "factschema",
		"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode": "schemadecode",
	}

	parsedPerDir := make(map[string]int)
	watchedImports := 0
	for _, dir := range decodeSurfaceDirs(t) {
		parsedPerDir[dir] = walkParsedGoFiles(t, dir, func(path string, file *ast.File) {
			for _, imported := range file.Imports {
				importPath := strings.Trim(imported.Path.Value, `"`)
				want, watched := wantIdentifier[importPath]
				if !watched {
					continue
				}
				watchedImports++
				if imported.Name == nil || imported.Name.Name == want {
					continue
				}
				t.Errorf("%s imports %s as %q, but KnownDecodeQualifiers matches the bare identifier %q: every qualified decode call in this file is dropped from the payload-usage manifest with no failure. Import it without an alias, or add %q to KnownDecodeQualifiers.",
					path, importPath, imported.Name.Name, want, imported.Name.Name)
			}
		})
	}
	requireNonEmptySurfaces(t, parsedPerDir)

	if watchedImports == 0 {
		t.Fatal("no scanned file imports factschema or schemadecode: this guard would pass vacuously, so the surface list or the import paths are stale")
	}
}
