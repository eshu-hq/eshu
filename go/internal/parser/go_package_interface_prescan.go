package parser

import (
	"fmt"
	"path/filepath"
	"strings"

	golangparser "github.com/eshu-hq/eshu/go/internal/parser/golang"
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
		mergeGoImportedInterfaceParamMethods(results[packageDir], targets)
	}
	return results, nil
}

func (e *Engine) goImportedInterfaceParamMethodsForPath(path string) (GoImportedInterfaceParamMethods, error) {
	parser, err := e.runtime.Parser("go")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	targets, err := golangparser.ImportedInterfaceParamMethods(parser, path)
	return GoImportedInterfaceParamMethods(targets), err
}

func mergeGoImportedInterfaceParamMethods(
	target GoImportedInterfaceParamMethods,
	source GoImportedInterfaceParamMethods,
) {
	for functionName, byIndex := range source {
		if _, ok := target[functionName]; !ok {
			target[functionName] = make(map[int][]string)
		}
		for index, methods := range byIndex {
			target[functionName][index] = appendUniqueGoMethods(target[functionName][index], methods)
		}
	}
}

func appendUniqueGoMethods(target []string, methods []string) []string {
	for _, method := range methods {
		trimmed := strings.TrimSpace(strings.ToLower(method))
		if trimmed == "" {
			continue
		}
		found := false
		for _, existing := range target {
			if existing == trimmed {
				found = true
				break
			}
		}
		if !found {
			target = append(target, trimmed)
		}
	}
	return target
}
