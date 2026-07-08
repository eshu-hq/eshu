// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_go "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// goParseDumpVariant names one shared.Options combination exercised by
// TestDumpGoParseCorpus. The set intentionally covers the branches that
// change Parse's output shape: source indexing, dataflow emission, and
// module-level variable scope.
type goParseDumpVariant struct {
	name    string
	options shared.Options
}

func goParseDumpVariants() []goParseDumpVariant {
	return []goParseDumpVariant{
		{name: "default", options: shared.Options{}},
		{name: "index_source", options: shared.Options{IndexSource: true}},
		{name: "emit_dataflow", options: shared.Options{EmitDataflow: true}},
		{name: "module_scope", options: shared.Options{VariableScope: "module"}},
	}
}

// goParseDumpCorpusRoots returns the directories to walk, relative to this
// package's directory (go/internal/parser/golang). GO_PARSE_CORPUS, if set,
// is a colon-joined list of roots (absolute or relative to this package's
// directory) that overrides the default set.
//
// The default is go/internal/parser itself (this package's parent): a few
// hundred diverse real Go files (generics, interfaces, closures, struct
// literals, dataflow-heavy code) that is representative enough to prove
// parser-output equivalence while keeping the dump a fast, synchronous,
// well-under-a-minute run.
func goParseDumpCorpusRoots() []string {
	if env := strings.TrimSpace(os.Getenv("GO_PARSE_CORPUS")); env != "" {
		parts := strings.Split(env, ":")
		roots := make([]string, 0, len(parts))
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				roots = append(roots, trimmed)
			}
		}
		return roots
	}
	return []string{".."} // go/internal/parser
}

// TestDumpGoParseCorpus is a guarded differential-equivalence harness for
// issue #4839 (collapsing the Go parser's per-file full-tree walks). It is
// not a regular regression test: it only runs when GO_PARSE_DUMP names an
// output path, and it is otherwise skipped so it adds no cost to normal
// `go test` runs.
//
// It parses every *.go file under the corpus roots (see
// goParseDumpCorpusRoots), skipping vendor/, .gocache/, and *_generated.go
// paths, with each variant in goParseDumpVariants, and writes a
// deterministically sorted "path\tvariant\tsha256(json)" index to
// GO_PARSE_DUMP. Running this dump before and after a refactor and diffing
// the two index files proves the parser's output stayed byte-for-byte
// identical across a representative, diverse slice of the repository's own
// Go sources.
func TestDumpGoParseCorpus(t *testing.T) {
	outPath := os.Getenv("GO_PARSE_DUMP")
	if outPath == "" {
		t.Skip("set GO_PARSE_DUMP=<path> to run the parser equivalence dump")
	}

	roots := goParseDumpCorpusRoots()
	files, err := goParseDumpCorpusFiles(roots)
	if err != nil {
		t.Fatalf("collect corpus files under %v: %v", roots, err)
	}
	if len(files) == 0 {
		t.Fatalf("no *.go files found under corpus roots %v", roots)
	}

	parser := tree_sitter.NewParser()
	if err := parser.SetLanguage(tree_sitter.NewLanguage(tree_sitter_go.Language())); err != nil {
		t.Fatalf("SetLanguage(go) error = %v", err)
	}

	variants := goParseDumpVariants()
	indexLines := make([]string, 0, len(files)*len(variants))

	for _, file := range files {
		rel := filepath.ToSlash(file)
		for _, variant := range variants {
			payload, parseErr := Parse(parser, file, false, variant.options)
			var sum [32]byte
			if parseErr != nil {
				sum = sha256.Sum256([]byte("ERROR:" + parseErr.Error()))
			} else {
				canon, marshalErr := json.Marshal(payload)
				if marshalErr != nil {
					t.Fatalf("marshal payload for %s (%s): %v", rel, variant.name, marshalErr)
				}
				sum = sha256.Sum256(canon)
			}
			indexLines = append(indexLines, fmt.Sprintf("%s\t%s\t%s", rel, variant.name, hex.EncodeToString(sum[:])))
		}
	}

	sort.Strings(indexLines)

	if err := os.WriteFile(outPath, []byte(strings.Join(indexLines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write dump index %q: %v", outPath, err)
	}

	t.Logf("dumped %d files x %d variants = %d entries to %s", len(files), len(variants), len(indexLines), outPath)
}

// goParseDumpCorpusFiles returns every non-skipped *.go file under roots,
// deduplicated by absolute path and sorted for deterministic traversal order.
// It skips vendor/, .gocache/, node_modules/, and .git/ directories and any
// *_generated.go file so the dump stays representative without paying for
// vendored or machine-generated sources.
func goParseDumpCorpusFiles(roots []string) ([]string, error) {
	seen := make(map[string]struct{})
	var files []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				switch d.Name() {
				case "vendor", ".gocache", "node_modules", ".git":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_generated.go") {
				return nil
			}
			abs, absErr := filepath.Abs(path)
			if absErr != nil {
				abs = path
			}
			if _, ok := seen[abs]; ok {
				return nil
			}
			seen[abs] = struct{}{}
			files = append(files, path)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	return files, nil
}
