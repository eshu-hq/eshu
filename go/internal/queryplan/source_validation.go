// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queryplan

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ValidateManifestSources verifies that every declared query fragment or source
// digest remains bound to its owning production Go function or value symbol.
func ValidateManifestSources(manifest Manifest, repoRoot string) error {
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	var violations []string
	for _, entry := range manifest.Entries {
		queryFragment := strings.TrimSpace(entry.QueryFragment)
		if queryFragment == "" && strings.TrimSpace(entry.Source.SourceSHA256) == "" {
			continue
		}
		path, err := manifestSourcePath(root, entry.Source.File)
		if err != nil {
			violations = append(violations, fmt.Sprintf("%s: %v", entry.ID, err))
			continue
		}
		source, err := os.ReadFile(path) // #nosec G304 -- path is constrained beneath the repository root and comes from an internal manifest
		if err != nil {
			violations = append(violations, fmt.Sprintf("%s: read source: %v", entry.ID, err))
			continue
		}
		symbolSource, err := manifestSymbolSource(path, source, entry.Source.Symbol)
		if err != nil {
			violations = append(violations, fmt.Sprintf("%s: %v", entry.ID, err))
			continue
		}
		if queryFragment != "" && !strings.Contains(normalizeCypher(symbolSource), normalizeCypher(queryFragment)) {
			violations = append(violations, fmt.Sprintf(
				"%s: query_fragment is absent from source symbol %s",
				entry.ID,
				entry.Source.Symbol,
			))
		}
		if entry.Source.SourceSHA256 != "" {
			digest := fmt.Sprintf("%x", sha256.Sum256([]byte(symbolSource)))
			if digest != entry.Source.SourceSHA256 {
				violations = append(violations, fmt.Sprintf(
					"%s: source_sha256 does not match source symbol %s (manifest %s, production %s)",
					entry.ID,
					entry.Source.Symbol,
					entry.Source.SourceSHA256,
					digest,
				))
			}
		}
	}
	if len(violations) > 0 {
		sort.Strings(violations)
		return errors.New(strings.Join(violations, "; "))
	}
	return nil
}

func manifestSourcePath(repoRoot, sourceFile string) (string, error) {
	if strings.TrimSpace(sourceFile) == "" {
		return "", fmt.Errorf("missing source file")
	}
	path, err := filepath.Abs(filepath.Join(repoRoot, filepath.Clean(sourceFile)))
	if err != nil {
		return "", fmt.Errorf("resolve source file: %w", err)
	}
	relative, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return "", fmt.Errorf("compare source file to repository root: %w", err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source file escapes repository root")
	}
	return path, nil
}

func manifestSymbolSource(path string, source []byte, symbol string) (string, error) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, path, source, 0)
	if err != nil {
		return "", fmt.Errorf("parse source: %w", err)
	}
	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			if functionSymbol(typed) == symbol {
				return sourceNodeText(fileSet, source, typed, symbol)
			}
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				valueSpec, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for _, name := range valueSpec.Names {
					if name.Name == symbol {
						return sourceNodeText(fileSet, source, typed, symbol)
					}
				}
			}
		}
	}
	return "", fmt.Errorf("source symbol %s not found", symbol)
}

func sourceNodeText(fileSet *token.FileSet, source []byte, node ast.Node, symbol string) (string, error) {
	start := fileSet.Position(node.Pos()).Offset
	end := fileSet.Position(node.End()).Offset
	if start < 0 || end < start || end > len(source) {
		return "", fmt.Errorf("invalid source offsets for symbol %s", symbol)
	}
	return string(source[start:end]), nil
}
