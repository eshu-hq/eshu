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
// The tests below make that drift fail loudly instead.

// decodePackageImports maps each real decode package's import path to the
// bare package identifier it is expected to be imported under, matching
// KnownDecodeQualifiers. Both drift tests below key off this single map: the
// alias test flags a file that imports one of these paths under any other
// name, and the qualifier test uses it to tell a genuine decode-package
// selector apart from an unrelated package whose member happens to share a
// seam's name (#6382 review finding 3).
var decodePackageImports = map[string]string{
	"github.com/eshu-hq/eshu/sdk/go/factschema":                "factschema",
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode": "schemadecode",
}

// decodeSurfaceDirs returns the six trees Load feeds to ScanDecodeUsage, by
// calling decodeScanDirs (load.go) against this test's own resolved Paths --
// the exact same function Load's own scan loop iterates, rather than a
// hand-written list living only in this test file.
//
// The first version of this helper hard-coded the six paths itself and got
// two of them wrong: "go/internal/loader", which does not exist (LoaderDir
// actually resolves to go/internal/storage/postgres), and
// "go/internal/replay", a superset of the real go/internal/replay/offlinetier.
// Because walkParsedGoFiles tolerates a missing directory, the wrong one
// contributed zero files and nothing said so -- and the surface it silently
// dropped is the one KnownDecodeQualifiers' own doc comment cites as the
// reason "factschema" is on the allowlist at all. The three mutations this
// file is meant to catch were still caught, but only because those
// qualifiers happen to be used in the correctly-named trees too. A guard
// that reads a path nobody maintains is not guarding the thing it names.
//
// A second defect had the same shape one level up: len(dirs) == 6 was
// asserted against this same hand-written list, so a seventh surface added
// to Load's own scan would silently stay unseen by this test too. Routing
// both Load and this helper through decodeScanDirs closes that: a surface
// added to Load's list is, by construction, added to this test's list in the
// same edit (#6382 review finding 2).
func decodeSurfaceDirs(t *testing.T) []string {
	t.Helper()

	return decodeScanDirs(ResolvePaths(Paths{RepoRoot: repoRoot(t)}))
}

// requireNonEmptySurfaces fails when any surface contributed zero parsed files.
//
// walkParsedGoFiles tolerates a missing ROOT directory on purpose, so that a
// tree relocated by the in-flight #6061 restructure does not turn this guard
// red for a reason that has nothing to do with qualifier drift. That
// tolerance is also how a wrong path hides, so the count per surface is
// asserted here instead: tolerate a moved tree, never tolerate a silently
// unscanned one.
func requireNonEmptySurfaces(t *testing.T, parsedPerDir map[string]int) {
	t.Helper()

	for dir, parsed := range parsedPerDir {
		if parsed == 0 {
			t.Errorf("decode surface %s contributed zero parsed Go files: this guard is not reading it, so any qualifier or alias drift there is invisible. Check the path against ResolvePaths in paths.go.", dir)
		}
	}
}

// walkParsedGoFiles parses every non-test .go file under dir and hands each
// one to visit, returning how many files it parsed so a caller can prove the
// walk was not empty.
//
// A missing ROOT directory is tolerated, not reported: these trees move
// between packages during the #6061 restructure, and treating a relocated
// tree as an error would be noise rather than signal --
// requireNonEmptySurfaces is what actually asserts a surface contributed no
// files, which is the failure that matters here. A walk or parse error on a
// path INSIDE an existing directory is a different failure mode -- the tree
// exists but this guard could not read part of it -- and previously was
// dropped exactly the same way as the tolerated missing-root case, which
// made an unreadable or unparseable file invisible to a guard whose whole
// purpose is failing loudly when scanning is incomplete. Both are now
// reported via t.Errorf while the walk continues over the rest of the tree,
// so one bad file does not hide drift in its siblings (#6382 review
// finding 4).
func walkParsedGoFiles(t *testing.T, dir string, visit func(path string, file *ast.File)) int {
	t.Helper()

	fset := token.NewFileSet()
	parsed := 0
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if path == dir {
				// The root itself is missing or unreadable: tolerated, per
				// the doc comment above.
				return nil
			}
			t.Errorf("walk %s: %v: this guard could not read part of %s, so any qualifier or alias drift under this path is invisible for this run", path, walkErr, dir)
			return nil
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, 0)
		if parseErr != nil {
			t.Errorf("parse %s: %v: this guard could not parse this file, so any qualifier or alias drift in it is invisible for this run", path, parseErr)
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

// decodePackageIdentsInFile returns the set of identifiers file binds, via
// its own import block, to one of decodePackageImports' real decode-package
// paths -- accounting for an explicit import alias, and falling back to the
// canonical bare identifier when the import is unaliased.
//
// A qualifier identifier NOT in this set is not bound to a real decode
// package in this file at all: a same-named method on an unrelated package
// (e.g. codec.DecodeAWSResource, where "codec" is nothing to do with
// factschema or schemadecode). Production's decodeCallName rejects that
// selector's qualifier outright before decodeFuncs is ever consulted, so
// this guard must not treat it as a decode call either (#6382 review
// finding 3).
func decodePackageIdentsInFile(file *ast.File) map[string]struct{} {
	idents := make(map[string]struct{}, len(decodePackageImports))
	for _, imported := range file.Imports {
		importPath := strings.Trim(imported.Path.Value, `"`)
		canonical, isDecodePackage := decodePackageImports[importPath]
		if !isDecodePackage {
			continue
		}
		if imported.Name != nil {
			idents[imported.Name.Name] = struct{}{}
			continue
		}
		idents[canonical] = struct{}{}
	}
	return idents
}

// unknownQualifierSeamCall reports whether selector -- found in file -- is a
// call through a qualifier that (a) resolves via file's own imports to a
// real decode package (decodePackageImports) and (b) is not itself a member
// of known. That is exactly the shape decodeCallName in production would
// drop: a real decode-package call site reached through a spelling
// KnownDecodeQualifiers does not recognize.
//
// A qualified call site commonly uses the seam's still-exported subpackage
// spelling (e.g. factschema.DecodeAWSResource), while seamNames is keyed by
// each seam's ROOT identity after ResolveForwardedSeams (e.g.
// "decodeAWSResource" when a root forwarder still exists). forwarders
// resolves that gap exactly the way production's decodeCallName does: an
// exported Sel.Name with a forwarders entry maps to its root name before the
// seamNames lookup, so this check targets the same identity production
// attributes the call to, not the as-written spelling.
//
// A selector whose qualifier does NOT resolve to a real decode package at
// all -- an unrelated package's coincidentally same-named method -- is never
// flagged, matching production's behavior of rejecting the qualifier before
// decodeFuncs is ever consulted (#6382 review finding 3). When flagged,
// qualifier is the identifier name to report.
func unknownQualifierSeamCall(file *ast.File, selector *ast.SelectorExpr, seamNames map[string]struct{}, forwarders RootForwarders, known DecodeQualifiers) (qualifier string, flagged bool) {
	ident, isIdent := selector.X.(*ast.Ident)
	if !isIdent || !isDecodeSeamCallName(selector.Sel.Name) {
		return "", false
	}
	resolvedName := selector.Sel.Name
	if rootName, hasForwarder := forwarders[selector.Sel.Name]; hasForwarder {
		resolvedName = rootName
	}
	if _, isSeam := seamNames[resolvedName]; !isSeam {
		return "", false
	}
	if _, boundToDecodePackage := decodePackageIdentsInFile(file)[ident.Name]; !boundToDecodePackage {
		return "", false
	}
	if _, isKnown := known[ident.Name]; isKnown {
		return ident.Name, false
	}
	return ident.Name, true
}

// TestNoDecodeSeamIsReachedThroughAnUnknownQualifier fails when a real decode
// seam is called through a package qualifier KnownDecodeQualifiers omits.
//
// It deliberately does NOT assert that every `x.DecodeFoo(...)` call uses a
// known qualifier. Unrelated packages legitimately expose Decode-prefixed
// methods, so that assertion would be a false alarm rather than a guard. The
// real invariant is narrower: when a call names a function that IS one of
// the parsed seams AND is reached through a qualifier bound to a real decode
// package, that qualifier must be recognized -- otherwise decodeCallName
// drops the call and its field reads disappear from the manifest with
// nothing turning red. See unknownQualifierSeamCall.
func TestNoDecodeSeamIsReachedThroughAnUnknownQualifier(t *testing.T) {
	t.Parallel()

	// Resolve the FULL merged seam set the way Load does -- across all six
	// surfaces (reducer, projector, query, loader, relationships, replay),
	// with root-forwarder resolution applied -- rather than re-deriving a
	// reducer-only subset here: a seam that exists only in a non-reducer
	// surface would otherwise never appear in seamNames below, so drift on
	// that seam's qualifier would be invisible to this guard.
	// resolveAllDecodeSeams (load.go) is the single path Load itself uses
	// (#6382 review finding 1).
	resolved := ResolvePaths(Paths{RepoRoot: repoRoot(t)})
	seams, forwarders, err := resolveAllDecodeSeams(resolved)
	if err != nil {
		t.Fatalf("resolveAllDecodeSeams() error = %v", err)
	}

	seamNames := make(map[string]struct{}, len(seams))
	for _, seam := range seams {
		seamNames[seam.FuncName] = struct{}{}
	}
	if len(seamNames) == 0 {
		t.Fatal("resolved zero decode seams: this guard would pass vacuously, so the seam resolution no longer finds any")
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
				if qualifier, flagged := unknownQualifierSeamCall(file, selector, seamNames, forwarders, KnownDecodeQualifiers); flagged {
					t.Errorf("%s calls decode seam %s through qualifier %q, which KnownDecodeQualifiers does not list: decodeCallName drops this call, so its field reads are missing from the payload-usage manifest with no failure anywhere. Add %q to KnownDecodeQualifiers, or route the call through a decode package already listed there.",
						path, selector.Sel.Name, qualifier, qualifier)
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

// TestUnknownQualifierSeamCallIgnoresUnrelatedPackage proves
// unknownQualifierSeamCall does not flag a selector whose qualifier is not
// bound, in the file, to a real decode package -- a same-named method on an
// unrelated package, which production's decodeCallName also ignores (it
// rejects the qualifier before decodeFuncs is ever consulted). Without the
// decodePackageIdentsInFile resolution, this same fixture would be flagged
// as an unknown-qualifier decode call, a false alarm on a function that has
// nothing to do with the real decode seam of the same name (#6382 review
// finding 3).
func TestUnknownQualifierSeamCallIgnoresUnrelatedPackage(t *testing.T) {
	t.Parallel()

	const src = `package fixture

import "example.com/codec"

func useIt() {
	codec.DecodeAWSResource(nil)
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	seamNames := map[string]struct{}{"DecodeAWSResource": {}}

	var selector *ast.SelectorExpr
	ast.Inspect(file, func(node ast.Node) bool {
		if sel, ok := node.(*ast.SelectorExpr); ok {
			selector = sel
		}
		return true
	})
	if selector == nil {
		t.Fatal("fixture has no selector expression to test")
	}

	if qualifier, flagged := unknownQualifierSeamCall(file, selector, seamNames, nil, KnownDecodeQualifiers); flagged {
		t.Fatalf("unknownQualifierSeamCall flagged codec.DecodeAWSResource through qualifier %q: %q is not imported from a real decode package in this file, so this is a coincidental same-named method, not a decode call this guard should police", qualifier, qualifier)
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

	parsedPerDir := make(map[string]int)
	watchedImports := 0
	for _, dir := range decodeSurfaceDirs(t) {
		parsedPerDir[dir] = walkParsedGoFiles(t, dir, func(path string, file *ast.File) {
			for _, imported := range file.Imports {
				importPath := strings.Trim(imported.Path.Value, `"`)
				want, watched := decodePackageImports[importPath]
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
