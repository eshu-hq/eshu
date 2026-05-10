package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	golangparser "github.com/eshu-hq/eshu/go/internal/parser/golang"
)

// PreScanGoPackageSemanticRoots returns package-level Go reachability evidence
// that must be collected before per-file parsing. The result includes package
// import paths, imported interface parameter contracts, imported receiver call
// roots, chained interface-return receiver roots, and generic constraint roots.
// The collector feeds these contracts back into per-file parsing so symbol
// roots can be bounded by package and receiver evidence.
func (e *Engine) PreScanGoPackageSemanticRoots(
	repoRoot string,
	paths []string,
) (GoPackageSemanticRoots, error) {
	resolvedRepoRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve go package interface prescan repo root %q: %w", repoRoot, err)
	}

	results := make(GoPackageSemanticRoots)
	packageImportPaths := make(map[string]string)
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
		packageDir := filepath.Dir(resolvedPath)
		options := results[packageDir]
		if options.ImportedInterfaceParamMethods == nil {
			options.ImportedInterfaceParamMethods = make(GoImportedInterfaceParamMethods)
		}
		if options.DirectMethodCallRoots == nil {
			options.DirectMethodCallRoots = make(GoDirectMethodCallRoots)
		}
		results[packageDir] = options
		targets, err := e.goImportedInterfaceParamMethodsForPath(resolvedPath)
		if err != nil {
			return nil, err
		}
		if len(targets) > 0 {
			options := results[packageDir]
			mergeGoImportedInterfaceParamMethods(options.ImportedInterfaceParamMethods, targets)
			results[packageDir] = options
		}
	}

	modulePath := goModulePath(resolvedRepoRoot)
	if modulePath != "" {
		for packageDir := range results {
			if importPath, ok := goImportPathForDir(resolvedRepoRoot, modulePath, packageDir); ok {
				options := results[packageDir]
				options.ImportPath = importPath
				results[packageDir] = options
				packageImportPaths[packageDir] = importPath
			}
		}
	}
	qualifiedTargets, err := e.goQualifiedImportedPackageInterfaceParamMethods(resolvedRepoRoot, paths)
	if err != nil {
		return nil, err
	}
	for packageDir := range results {
		options := results[packageDir]
		mergeGoImportedInterfaceParamMethods(options.ImportedInterfaceParamMethods, qualifiedTargets)
		results[packageDir] = options
	}
	directMethodRoots, err := e.goQualifiedImportedPackageDirectMethodCallRoots(paths)
	if err != nil {
		return nil, err
	}
	packageInterfaceReturns, err := e.goPackageLocalInterfaceImportedMethodReturns(paths)
	if err != nil {
		return nil, err
	}
	chainedDirectMethodRoots, err := e.goQualifiedImportedPackageDirectMethodCallRootsWithInterfaceReturns(paths, packageInterfaceReturns)
	if err != nil {
		return nil, err
	}
	mergeGoDirectMethodCallRoots(directMethodRoots, chainedDirectMethodRoots)
	for packageDir, importPath := range packageImportPaths {
		options := results[packageDir]
		mergeGoDirectMethodCallRootsForImportPath(options.DirectMethodCallRoots, directMethodRoots, importPath)
		results[packageDir] = options
	}
	if err := e.mergeGoPackageGenericConstraintMethodRoots(paths, packageImportPaths, results); err != nil {
		return nil, err
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

func (e *Engine) goQualifiedImportedPackageInterfaceParamMethods(
	repoRoot string,
	paths []string,
) (GoImportedInterfaceParamMethods, error) {
	modulePath := goModulePath(repoRoot)
	if modulePath == "" {
		return nil, nil
	}

	qualified := make(GoImportedInterfaceParamMethods)
	for _, rawPath := range paths {
		resolvedPath, err := filepath.Abs(rawPath)
		if err != nil {
			return nil, fmt.Errorf("resolve go qualified interface prescan path %q: %w", rawPath, err)
		}
		definition, ok := e.registry.LookupByPath(resolvedPath)
		if !ok || definition.Language != "go" {
			continue
		}
		importPath, ok := goImportPathForDir(repoRoot, modulePath, filepath.Dir(resolvedPath))
		if !ok {
			continue
		}
		targets, err := e.goExportedInterfaceParamMethodsForPath(resolvedPath)
		if err != nil {
			return nil, err
		}
		for functionName, byIndex := range targets {
			key := strings.ToLower(importPath + "." + functionName)
			if _, ok := qualified[key]; !ok {
				qualified[key] = make(map[int][]string)
			}
			for index, methods := range byIndex {
				qualified[key][index] = appendUniqueGoMethods(qualified[key][index], methods)
			}
		}
	}
	return qualified, nil
}

func (e *Engine) goQualifiedImportedPackageDirectMethodCallRoots(paths []string) (GoDirectMethodCallRoots, error) {
	roots := make(GoDirectMethodCallRoots)
	for _, rawPath := range paths {
		resolvedPath, err := filepath.Abs(rawPath)
		if err != nil {
			return nil, fmt.Errorf("resolve go method call root prescan path %q: %w", rawPath, err)
		}
		definition, ok := e.registry.LookupByPath(resolvedPath)
		if !ok || definition.Language != "go" {
			continue
		}
		fileRoots, err := e.goImportedDirectMethodCallRootsForPath(resolvedPath)
		if err != nil {
			return nil, err
		}
		mergeGoDirectMethodCallRoots(roots, fileRoots)
	}
	return roots, nil
}

func (e *Engine) goImportedDirectMethodCallRootsForPath(path string) (GoDirectMethodCallRoots, error) {
	parser, err := e.runtime.Parser("go")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	roots, err := golangparser.ImportedDirectMethodCallRoots(parser, path)
	return GoDirectMethodCallRoots(roots), err
}

func (e *Engine) goPackageLocalInterfaceImportedMethodReturns(paths []string) (map[string]map[string]string, error) {
	results := make(map[string]map[string]string)
	for _, rawPath := range paths {
		resolvedPath, err := filepath.Abs(rawPath)
		if err != nil {
			return nil, fmt.Errorf("resolve go interface return prescan path %q: %w", rawPath, err)
		}
		definition, ok := e.registry.LookupByPath(resolvedPath)
		if !ok || definition.Language != "go" {
			continue
		}
		returns, err := e.goLocalInterfaceImportedMethodReturnsForPath(resolvedPath)
		if err != nil {
			return nil, err
		}
		if len(returns) == 0 {
			continue
		}
		packageDir := filepath.Dir(resolvedPath)
		if results[packageDir] == nil {
			results[packageDir] = make(map[string]string)
		}
		for key, typeName := range returns {
			results[packageDir][key] = typeName
		}
	}
	return results, nil
}

func (e *Engine) goQualifiedImportedPackageDirectMethodCallRootsWithInterfaceReturns(
	paths []string,
	packageInterfaceReturns map[string]map[string]string,
) (GoDirectMethodCallRoots, error) {
	roots := make(GoDirectMethodCallRoots)
	for _, rawPath := range paths {
		resolvedPath, err := filepath.Abs(rawPath)
		if err != nil {
			return nil, fmt.Errorf("resolve go chained method call root prescan path %q: %w", rawPath, err)
		}
		definition, ok := e.registry.LookupByPath(resolvedPath)
		if !ok || definition.Language != "go" {
			continue
		}
		interfaceReturns := packageInterfaceReturns[filepath.Dir(resolvedPath)]
		if len(interfaceReturns) == 0 {
			continue
		}
		fileRoots, err := e.goImportedDirectMethodCallRootsWithInterfaceReturnsForPath(resolvedPath, interfaceReturns)
		if err != nil {
			return nil, err
		}
		mergeGoDirectMethodCallRoots(roots, fileRoots)
	}
	return roots, nil
}

func (e *Engine) goLocalInterfaceImportedMethodReturnsForPath(path string) (map[string]string, error) {
	parser, err := e.runtime.Parser("go")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	return golangparser.LocalInterfaceImportedMethodReturns(parser, path)
}

func (e *Engine) goImportedDirectMethodCallRootsWithInterfaceReturnsForPath(
	path string,
	interfaceMethodReturns map[string]string,
) (GoDirectMethodCallRoots, error) {
	parser, err := e.runtime.Parser("go")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	roots, err := golangparser.ImportedDirectMethodCallRootsWithInterfaceReturns(parser, path, interfaceMethodReturns)
	return GoDirectMethodCallRoots(roots), err
}

func (e *Engine) mergeGoPackageGenericConstraintMethodRoots(
	paths []string,
	packageImportPaths map[string]string,
	results GoPackageSemanticRoots,
) error {
	packageInterfaces := make(map[string]map[string][]string)
	packageConstraints := make(map[string][]string)
	packageMethods := make(map[string][]string)

	for _, rawPath := range paths {
		resolvedPath, err := filepath.Abs(rawPath)
		if err != nil {
			return fmt.Errorf("resolve go generic constraint prescan path %q: %w", rawPath, err)
		}
		definition, ok := e.registry.LookupByPath(resolvedPath)
		if !ok || definition.Language != "go" {
			continue
		}
		packageDir := filepath.Dir(resolvedPath)
		interfaceMethods, err := e.goLocalInterfaceMethodsForPath(resolvedPath)
		if err != nil {
			return err
		}
		if len(interfaceMethods) > 0 && packageInterfaces[packageDir] == nil {
			packageInterfaces[packageDir] = make(map[string][]string)
		}
		for name, methods := range interfaceMethods {
			packageInterfaces[packageDir][name] = appendUniqueGoMethods(packageInterfaces[packageDir][name], methods)
		}
		constraints, err := e.goGenericConstraintInterfaceNamesForPath(resolvedPath)
		if err != nil {
			return err
		}
		packageConstraints[packageDir] = appendUniqueGoMethods(packageConstraints[packageDir], constraints)
		methods, err := e.goMethodDeclarationKeysForPath(resolvedPath)
		if err != nil {
			return err
		}
		packageMethods[packageDir] = appendUniqueGoMethods(packageMethods[packageDir], methods)
	}

	for packageDir, importPath := range packageImportPaths {
		options := results[packageDir]
		if options.DirectMethodCallRoots == nil {
			options.DirectMethodCallRoots = make(GoDirectMethodCallRoots)
		}
		for _, constraint := range packageConstraints[packageDir] {
			requiredMethods := packageInterfaces[packageDir][constraint]
			if len(requiredMethods) == 0 {
				continue
			}
			for _, methodKey := range packageMethods[packageDir] {
				_, methodName, ok := strings.Cut(methodKey, ".")
				if !ok || !goMethodListContains(requiredMethods, methodName) {
					continue
				}
				qualifiedKey := strings.ToLower(importPath + "." + methodKey)
				options.DirectMethodCallRoots[qualifiedKey] = appendUniqueGoMethods(
					options.DirectMethodCallRoots[qualifiedKey],
					[]string{"go.generic_constraint_method"},
				)
			}
		}
		results[packageDir] = options
	}
	return nil
}

func (e *Engine) goLocalInterfaceMethodsForPath(path string) (map[string][]string, error) {
	parser, err := e.runtime.Parser("go")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	return golangparser.LocalInterfaceMethods(parser, path)
}

func (e *Engine) goGenericConstraintInterfaceNamesForPath(path string) ([]string, error) {
	parser, err := e.runtime.Parser("go")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	return golangparser.GenericConstraintInterfaceNames(parser, path)
}

func (e *Engine) goMethodDeclarationKeysForPath(path string) ([]string, error) {
	parser, err := e.runtime.Parser("go")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	return golangparser.MethodDeclarationKeys(parser, path)
}

func goMethodListContains(methods []string, method string) bool {
	normalized := strings.ToLower(strings.TrimSpace(method))
	for _, candidate := range methods {
		if candidate == normalized {
			return true
		}
	}
	return false
}

func (e *Engine) goExportedInterfaceParamMethodsForPath(path string) (GoImportedInterfaceParamMethods, error) {
	parser, err := e.runtime.Parser("go")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	targets, err := golangparser.ExportedInterfaceParamMethods(parser, path)
	return GoImportedInterfaceParamMethods(targets), err
}

func goModulePath(repoRoot string) string {
	body, err := os.ReadFile(filepath.Join(repoRoot, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return strings.TrimSpace(fields[1])
		}
	}
	return ""
}

func goImportPathForDir(repoRoot string, modulePath string, packageDir string) (string, bool) {
	rel, err := filepath.Rel(repoRoot, packageDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	if rel == "." {
		return modulePath, true
	}
	return modulePath + "/" + filepath.ToSlash(rel), true
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

func mergeGoDirectMethodCallRoots(target GoDirectMethodCallRoots, source GoDirectMethodCallRoots) {
	for key, kinds := range source {
		target[key] = appendUniqueGoMethods(target[key], kinds)
	}
}

func mergeGoDirectMethodCallRootsForImportPath(
	target GoDirectMethodCallRoots,
	source GoDirectMethodCallRoots,
	importPath string,
) {
	prefix := strings.ToLower(strings.TrimSpace(importPath)) + "."
	for key, kinds := range source {
		if strings.HasPrefix(key, prefix) {
			target[key] = appendUniqueGoMethods(target[key], kinds)
		}
	}
}
