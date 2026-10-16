// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// queryImportRoot is the import path of go/internal/query; imports under it
// resolve to a directory of the guard walk.
const queryImportRoot = "github.com/eshu-hq/eshu/go/internal/query"

// queryGuardFile is one parsed non-test file of a guard walk.
type queryGuardFile struct {
	rel  string // slash path relative to the walk root
	dir  string // slash directory relative to the walk root, "." for the root
	file *ast.File
	// imports maps each import's local name to its directory relative to the
	// walk root, or to "" for an import outside the walk root.
	imports map[string]string
}

// queryGuardTree is every non-test file under a walk root plus the writers
// derived from them: for each package directory, the functions that forward a
// status or a message into a response writer (queryWriterSpec).
type queryGuardTree struct {
	fset      *token.FileSet
	files     []*queryGuardFile
	writers   map[string]map[string]queryWriterSpec
	returners map[string]map[string]int // dir -> function -> result index of an allowed status
}

// loadQueryGuardTree parses every non-test .go file under root, skipping
// testdata and dot directories, in lexical walk order, and derives the
// writers. importRoot is the import path that root stands for. A file that
// does not parse fails t.
func loadQueryGuardTree(t *testing.T, root, importRoot string) *queryGuardTree {
	t.Helper()

	tree := &queryGuardTree{fset: token.NewFileSet()}
	walkErr := filepath.WalkDir(root, func(p string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if p != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(p) != ".go" || hasTestSuffix(p) {
			return nil
		}
		contents, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return relErr
		}
		tree.add(t, filepath.ToSlash(rel), contents, importRoot)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}
	tree.deriveReturners()
	tree.deriveWriters()
	return tree
}

// add parses one file into the tree; it does not derive writers.
func (tree *queryGuardTree) add(t *testing.T, rel string, src []byte, importRoot string) {
	t.Helper()

	file, err := parser.ParseFile(tree.fset, rel, src, 0)
	if err != nil {
		t.Errorf("parse %s: %v", rel, err)
		return
	}
	guardFile := &queryGuardFile{rel: rel, dir: path.Dir(rel), file: file, imports: map[string]string{}}
	for _, spec := range file.Imports {
		importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
		if unquoteErr != nil {
			continue
		}
		name := path.Base(importPath)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		switch {
		case importPath == importRoot:
			guardFile.imports[name] = "."
		case strings.HasPrefix(importPath, importRoot+"/"):
			guardFile.imports[name] = strings.TrimPrefix(importPath, importRoot+"/")
		default:
			guardFile.imports[name] = ""
		}
	}
	tree.files = append(tree.files, guardFile)
}

// queryMessageMode says when a writer's message argument reaches a body that
// the status makes a server failure. Larger is worse; merges keep the max.
type queryMessageMode int

const (
	queryMessageNone      queryMessageMode = iota
	queryMessageClient                     // always behind a literal 1xx-4xx status
	queryMessageStatusArg                  // behind the writer's own status argument
	queryMessageServer                     // behind a status that is not a literal 1xx-4xx
)

// queryWriterSpec describes how a writer uses its arguments. status is the
// index of the argument written as the HTTP status (-1 when there is none),
// and bit i of messages is set when argument i reaches the response body.
// fixed is
// the disallowed status the writer itself writes, and fixedChain the call path
// to that write. chain is the call path from the writer to the response sink.
type queryWriterSpec struct {
	status      int
	messages    uint64
	messageMode queryMessageMode
	chain       string
	fixed       string
	fixedChain  string
	fromTree    bool // derived from a function in the tree, not a base sink
}

// queryBaseWriters are the response sinks under any qualifier or receiver.
// Body-free sinks set no message bit: WriteErrorEnvelope's envelope is covered
// by the ErrorEnvelope literal rule, and WriteSuccess writes data.
var queryBaseWriters = map[string]queryWriterSpec{
	"WriteError":         {status: 1, messages: 1 << 2, messageMode: queryMessageStatusArg},
	"WriteContractError": {status: 2, messages: 1 << 3, messageMode: queryMessageStatusArg},
	"writeContractError": {status: 2, messages: 1 << 3, messageMode: queryMessageStatusArg},
	"WriteJSON":          {status: 1, messages: 1 << 2, messageMode: queryMessageStatusArg},
	"WriteErrorEnvelope": {status: 2},
	"WriteSuccess":       {status: 2},
	"WriteHeader":        {status: 0},
}

// queryHTTPErrorWriter is http.Error(w, message, status).
var queryHTTPErrorWriter = queryWriterSpec{status: 2, messages: 1 << 1, messageMode: queryMessageStatusArg}

// queryCallee names the function call invokes: name as written (pkg.Name for
// a qualified call), base its last element, and dir the package directory a
// tree function would live in ("" for an import outside the walk root).
// http.Error reports dir "http".
func queryCallee(f *queryGuardFile, call *ast.CallExpr) (name, base, dir string, ok bool) {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return fun.Name, fun.Name, f.dir, true
	case *ast.SelectorExpr:
		name, dir = fun.Sel.Name, f.dir
		if x, isIdent := fun.X.(*ast.Ident); isIdent {
			if x.Name == "http" && name == "Error" {
				return "http.Error", "Error", "http", true
			}
			if importDir, imported := f.imports[x.Name]; imported {
				return x.Name + "." + name, name, importDir, true
			}
		}
		return name, name, dir, true
	default:
		return "", "", "", false
	}
}

// resolve returns the spec of the writer call invokes, the name it is called
// by, and whether it is a writer. A base sink matches by name; a bare name or
// a method selector resolves in f's own package; pkg.Name resolves in the
// imported package when it is under the walk root.
func (tree *queryGuardTree) resolve(f *queryGuardFile, call *ast.CallExpr) (queryWriterSpec, string, bool) {
	name, base, dir, ok := queryCallee(f, call)
	switch {
	case !ok:
		return queryWriterSpec{}, "", false
	case dir == "http":
		return queryHTTPErrorWriter, name, true
	}
	if spec, ok := queryBaseWriters[base]; ok {
		return spec, name, true
	}
	if dir == "" {
		return queryWriterSpec{}, "", false
	}
	spec, ok := tree.writers[dir][base]
	spec.fromTree = ok
	return spec, name, ok
}

// deriveWriters computes the fixed point of writer specs over every function
// declaration in the tree. Specs only grow (queryMergeWriterSpec), so the loop
// ends.
func (tree *queryGuardTree) deriveWriters() {
	tree.writers = map[string]map[string]queryWriterSpec{}
	for changed := true; changed; {
		changed = false
		for _, f := range tree.files {
			for _, decl := range f.file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Body == nil {
					continue
				}
				derived, ok := tree.deriveSpec(f, fn)
				if !ok {
					continue
				}
				pkg := tree.writers[f.dir]
				if pkg == nil {
					pkg = map[string]queryWriterSpec{}
					tree.writers[f.dir] = pkg
				}
				current, had := pkg[fn.Name.Name]
				merged := derived
				if had {
					merged = queryMergeWriterSpec(current, derived)
				}
				if !had || merged != current {
					pkg[fn.Name.Name] = merged
					changed = true
				}
			}
		}
	}
}

// queryMergeWriterSpec keeps the first status index, every message bit, the
// worst message mode, and the first fixed status.
func queryMergeWriterSpec(a, b queryWriterSpec) queryWriterSpec {
	if a.status < 0 {
		a.status, a.chain = b.status, b.chain
	}
	a.messages |= b.messages
	a.messageMode = max(a.messageMode, b.messageMode)
	if a.fixed == "" {
		a.fixed, a.fixedChain = b.fixed, b.fixedChain
	}
	return a
}

// queryGuardTreeFromSource builds a one-file tree, as package directory ".",
// for the shapes tests; the file's writers are derived as on the real tree.
func queryGuardTreeFromSource(t *testing.T, name, src string) (*queryGuardTree, *queryGuardFile) {
	t.Helper()

	tree := &queryGuardTree{fset: token.NewFileSet()}
	tree.add(t, name, []byte(src), queryImportRoot)
	if len(tree.files) != 1 {
		t.Fatalf("parse synthetic source %s:\n%s", name, src)
	}
	tree.deriveReturners()
	tree.deriveWriters()
	return tree, tree.files[0]
}
