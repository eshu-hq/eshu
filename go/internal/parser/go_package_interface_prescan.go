package parser

import (
	"fmt"
	"path/filepath"
	"strings"
)

// PreScanGoPackageImportedInterfaceParamMethods returns same-package Go
// function signatures that accept known imported interfaces. The collector
// feeds these contracts back into per-file parsing so call arguments in one file
// can root concrete methods required by a function signature in another file.
func (e *Engine) PreScanGoPackageImportedInterfaceParamMethods(
	repoRoot string,
	paths []string,
) (GoPackageImportedInterfaceParamMethods, error) {
	resolvedRepoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve go package interface prescan repo root %q: %w", repoRoot, err)
	}

	results := make(GoPackageImportedInterfaceParamMethods)
	for _, rawPath := range paths {
		resolvedPath, err := filepath.Abs(rawPath)
		if err != nil {
			return nil, fmt.Errorf("resolve go package interface prescan path %q: %w", rawPath, err)
		}
		rel, err := filepath.Rel(resolvedRepoRoot, resolvedPath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		definition, ok := e.registry.LookupByPath(resolvedPath)
		if !ok || definition.Language != "go" {
			continue
		}
		targets, err := e.goImportedInterfaceParamMethodsForPath(resolvedPath)
		if err != nil {
			return nil, err
		}
		if len(targets) == 0 {
			continue
		}
		packageDir := filepath.Dir(resolvedPath)
		if _, ok := results[packageDir]; !ok {
			results[packageDir] = make(GoImportedInterfaceParamMethods)
		}
		goMergeImportedInterfaceParamMethods(results[packageDir], targets)
	}
	return results, nil
}

func (e *Engine) goImportedInterfaceParamMethodsForPath(path string) (GoImportedInterfaceParamMethods, error) {
	parser, err := e.runtime.Parser("go")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	source, err := readSource(path)
	if err != nil {
		return nil, err
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse go file %q: parser returned nil tree", path)
	}
	defer tree.Close()

	return goFunctionParamImportedInterfaceMethods(tree.RootNode(), source), nil
}
