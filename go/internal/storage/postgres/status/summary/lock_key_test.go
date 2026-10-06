// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTryLockBindsTheWriterKeyAndReportsTheResult(t *testing.T) {
	t.Parallel()

	for _, acquired := range []bool{true, false} {
		q := &fakeQueryer{rows: &fakeRows{rows: [][]any{{acquired}}}}
		got, err := TryLock(context.Background(), q)
		if err != nil {
			t.Fatalf("TryLock() error = %v", err)
		}
		if got != acquired {
			t.Fatalf("TryLock() = %v, want %v", got, acquired)
		}
		if len(q.calls) != 1 || len(q.calls[0].args) != 1 || q.calls[0].args[0] != WriterLockKey {
			t.Fatalf("TryLock() calls = %#v, want one call binding WriterLockKey", q.calls)
		}
	}
}

func TestTryLockPropagatesQueryErrors(t *testing.T) {
	t.Parallel()

	boom := errors.New("connection reset")
	if _, err := TryLock(context.Background(), &fakeQueryer{err: boom}); !errors.Is(err, boom) {
		t.Fatalf("TryLock() error = %v, want it to wrap %v", err, boom)
	}
}

// TestWriterLockKeyIsUniqueAmongTheRepositoryAdvisoryKeys scans every non-test
// Go file under go/ for integer constants that look like advisory-lock keys
// and fails when WriterLockKey equals any of them. The scan reads source, so a
// key added elsewhere later is caught the next time this test runs.
func TestWriterLockKeyIsUniqueAmongTheRepositoryAdvisoryKeys(t *testing.T) {
	t.Parallel()

	root := goModuleRoot(t)
	own := filepath.Join(root, "internal", "storage", "postgres", "status", "summary")
	found := collectAdvisoryKeyConstants(t, root, own)
	// Sanity: the scan must see the keys the ruling names, or it is vacuous.
	for _, name := range []string{"deferredMaintenanceBarrierStateLockKey", "deferredMaintenanceBarrierLockKey"} {
		if _, ok := found[name]; !ok {
			t.Fatalf("scan did not find %s; the uniqueness check would be vacuous (found %d keys)", name, len(found))
		}
	}
	mine := big.NewInt(WriterLockKey)
	for name, value := range found {
		if value.Cmp(mine) == 0 {
			t.Errorf("WriterLockKey %#x collides with advisory key constant %s", int64(WriterLockKey), name)
		}
	}
	if WriterLockKey == 0 {
		t.Error("WriterLockKey must be non-zero")
	}
}

// TestAdvisoryKeyScanFlagsASeededCollision proves the scan can fail: a file
// that declares a lock-key constant with WriterLockKey's value is reported.
func TestAdvisoryKeyScanFlagsASeededCollision(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	source := "package seeded\n\nconst seededWriterLockKey = " + big.NewInt(WriterLockKey).String() + "\n"
	if err := os.WriteFile(filepath.Join(dir, "seeded.go"), []byte(source), 0o600); err != nil {
		t.Fatalf("write seeded file: %v", err)
	}
	found := collectAdvisoryKeyConstants(t, dir, "")
	value, ok := found["seededWriterLockKey"]
	if !ok || value.Cmp(big.NewInt(WriterLockKey)) != 0 {
		t.Fatalf("scan of a seeded collision = %v, want seededWriterLockKey == WriterLockKey", found)
	}
}

func goModuleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// collectAdvisoryKeyConstants returns every integer constant whose name
// mentions "lock" or "advisory" in the non-test Go files under root, skipping
// the skipDir tree. Constants with a non-literal value are ignored.
func collectAdvisoryKeyConstants(t *testing.T, root, skipDir string) map[string]*big.Int {
	t.Helper()
	found := map[string]*big.Int{}
	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path == skipDir && skipDir != "" {
				return filepath.SkipDir
			}
			switch entry.Name() {
			case "testdata", "node_modules", ".git", ".gocache":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if parseErr != nil {
			return nil // a file the scan cannot parse cannot declare a usable constant
		}
		for _, declaration := range file.Decls {
			gen, ok := declaration.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				valueSpec := spec.(*ast.ValueSpec)
				for i, name := range valueSpec.Names {
					lower := strings.ToLower(name.Name)
					if !strings.Contains(lower, "lock") && !strings.Contains(lower, "advisory") {
						continue
					}
					if i >= len(valueSpec.Values) {
						continue
					}
					literal, ok := valueSpec.Values[i].(*ast.BasicLit)
					if !ok || literal.Kind != token.INT {
						continue
					}
					value, ok := new(big.Int).SetString(strings.ReplaceAll(literal.Value, "_", ""), 0)
					if !ok {
						continue
					}
					found[name.Name] = value
				}
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}
	return found
}
