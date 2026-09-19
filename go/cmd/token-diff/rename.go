// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strconv"
	"strings"
)

// internalImportPrefix is the only import-path tree whose moves the rename
// exemption accepts. A path outside it is a dependency change, not a
// repository-internal package move.
const internalImportPrefix = "github.com/eshu-hq/eshu/go/internal/"

// importRename describes one repository-internal import substitution as
// seen from a single importing file: the import path changed from oldPath to newPath,
// and every use of the package qualifier changed from oldName to newName.
type importRename struct {
	oldPath, newPath string
	oldName, newName string
}

// String renders the rename for the tool's stdout report.
func (r importRename) String() string {
	return fmt.Sprintf("%s -> %s (qualifier %s. -> %s.)", r.oldPath, r.newPath, r.oldName, r.newName)
}

// fileParts splits one Go source file around its import declarations.
type fileParts struct {
	prefix, imports, suffix []byte
	// specs holds each import as "name\x00path"; name is empty when the
	// import has no alias.
	specs []string
}

// splitImports parses src's import declarations and splits the file into
// the bytes before the first import declaration, the declarations
// themselves, and the rest of the file. ok is false when src has no imports.
func splitImports(src []byte) (parts fileParts, ok bool, err error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "src.go", src, parser.ImportsOnly)
	if err != nil {
		return fileParts{}, false, err
	}
	if len(f.Decls) == 0 || len(f.Imports) == 0 {
		return fileParts{}, false, nil
	}
	start := fset.Position(f.Decls[0].Pos()).Offset
	end := fset.Position(f.Decls[len(f.Decls)-1].End()).Offset
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			return fileParts{}, false, err
		}
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name
		}
		parts.specs = append(parts.specs, name+"\x00"+p)
	}
	parts.prefix, parts.imports, parts.suffix = src[:start], src[start:end], src[end:]
	return parts, true, nil
}

// internalImportRenameOnly reports whether head equals base after exactly
// one repository-internal import substitution: one unaliased import path under
// internalImportPrefix replaced by another, and every qualified use of the
// old package name (oldName.X) replaced by the new one (newName.X), with no
// other token changed. Plain // comments are ignored, exactly as in the
// comment-only comparison. It fails closed: any shape it cannot prove is a
// pure rename returns ok == false.
//
// The names are the last path elements of the two import paths, so a
// package whose clause differs from its directory name never qualifies.
// Only one rename per file is accepted.
//
// Seeing one file, this cannot tell a package move from a swap between two
// packages that both exist. The caller must prove the move from the tree:
// scripts/lib/parser_relationship_comment_only_diff.sh checks that the old
// package directory disappeared and the new one appeared in the same diff.
func internalImportRenameOnly(baseSrc, headSrc []byte) (importRename, bool, error) {
	base, baseOK, err := splitImports(baseSrc)
	if err != nil || !baseOK {
		return importRename{}, false, err
	}
	head, headOK, err := splitImports(headSrc)
	if err != nil || !headOK {
		return importRename{}, false, err
	}

	removed, added := specDifference(base.specs, head.specs), specDifference(head.specs, base.specs)
	if len(removed) != 1 || len(added) != 1 {
		return importRename{}, false, nil
	}
	oldName, oldPath, _ := strings.Cut(removed[0], "\x00")
	newName, newPath, _ := strings.Cut(added[0], "\x00")
	if oldName != "" || newName != "" {
		return importRename{}, false, nil // aliased imports are out of scope.
	}
	if !strings.HasPrefix(oldPath, internalImportPrefix) || !strings.HasPrefix(newPath, internalImportPrefix) {
		return importRename{}, false, nil
	}
	r := importRename{oldPath: oldPath, newPath: newPath, oldName: path.Base(oldPath), newName: path.Base(newPath)}
	if !token.IsIdentifier(r.oldName) || !token.IsIdentifier(r.newName) {
		return importRename{}, false, nil
	}

	for _, region := range [][]byte{base.imports, head.imports} {
		if ok, err := plainImportTokens(region); err != nil || !ok {
			return importRename{}, false, err
		}
	}
	if ok, err := regionsEqual(base.prefix, head.prefix, nil); err != nil || !ok {
		return importRename{}, false, err
	}
	if ok, err := regionsEqual(base.suffix, head.suffix, &r); err != nil || !ok {
		return importRename{}, false, err
	}
	return r, true, nil
}

// specDifference returns the entries of a that b does not contain, counting
// duplicates, in sorted order.
func specDifference(a, b []string) []string {
	remaining := slices.Clone(b)
	var out []string
	for _, s := range a {
		if i := slices.Index(remaining, s); i >= 0 {
			remaining = slices.Delete(remaining, i, i+1)
			continue
		}
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// plainImportTokens reports whether an import-declaration region holds only
// the tokens an import block is made of. A kept comment (a block comment or
// directive) or anything else makes the region ineligible, because the
// import-set comparison would not see it.
func plainImportTokens(region []byte) (bool, error) {
	toks, err := tokenize(region)
	if err != nil {
		return false, err
	}
	for _, t := range toks {
		switch t.kind {
		case token.IMPORT, token.LPAREN, token.RPAREN, token.SEMICOLON, token.STRING, token.IDENT, token.PERIOD:
		default:
			return false, nil
		}
	}
	return true, nil
}

// regionsEqual compares two source regions' filtered token streams. When
// rename is non-nil, each qualifier use of rename.oldName in base is first
// rewritten to rename.newName. The rewrite is refused (false) when base uses
// oldName in any other position -- as a declared name, a selector field, or
// an operand -- or already uses newName anywhere, since either would make
// the substitution ambiguous.
func regionsEqual(base, head []byte, rename *importRename) (bool, error) {
	baseToks, err := tokenize(base)
	if err != nil {
		return false, err
	}
	headToks, err := tokenize(head)
	if err != nil {
		return false, err
	}
	if rename != nil && rename.oldName != rename.newName {
		for i, t := range baseToks {
			if t.kind != token.IDENT {
				continue
			}
			if t.lit == rename.newName {
				return false, nil
			}
			if t.lit != rename.oldName {
				continue
			}
			qualifier := i+1 < len(baseToks) && baseToks[i+1].kind == token.PERIOD &&
				(i == 0 || baseToks[i-1].kind != token.PERIOD)
			if !qualifier {
				return false, nil
			}
			baseToks[i].lit = rename.newName
		}
	}
	return tokensEqual(baseToks, headToks), nil
}
