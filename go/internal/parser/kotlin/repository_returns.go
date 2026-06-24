// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kotlin

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func kotlinCollectSiblingFunctionReturnTypes(repoRoot string, currentPath string, packageName string, parser *tree_sitter.Parser) (map[string]string, error) {
	root := filepath.Dir(currentPath)
	currentAbs, err := filepath.Abs(currentPath)
	if err != nil {
		return nil, err
	}
	boundedRepoRoot := strings.TrimSpace(repoRoot)
	if boundedRepoRoot != "" {
		if boundedRepoRoot, err = filepath.Abs(boundedRepoRoot); err != nil {
			return nil, err
		}
	}

	candidates := make(map[string]struct {
		value     string
		ambiguous bool
	})
	record := func(key string, returnType string) {
		key = strings.TrimSpace(key)
		returnType = strings.TrimSpace(returnType)
		if key == "" || returnType == "" {
			return
		}
		candidate, ok := candidates[key]
		if !ok {
			candidates[key] = struct {
				value     string
				ambiguous bool
			}{value: returnType}
			return
		}
		if candidate.value == returnType {
			return
		}
		candidate.ambiguous = true
		candidates[key] = candidate
	}

	roots := []string{root}
	for ancestor := filepath.Dir(root); ancestor != root && len(roots) < 4; ancestor = filepath.Dir(ancestor) {
		if boundedRepoRoot != "" {
			rel, relErr := filepath.Rel(boundedRepoRoot, ancestor)
			if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				break
			}
		}
		base := filepath.Base(ancestor)
		if base == "T" || strings.HasPrefix(base, "TemporaryItems") {
			break
		}
		roots = append(roots, ancestor)
	}
	for _, directory := range roots {
		functionReturnTypes, err := kotlinCollectFunctionReturnTypesFromDirectory(directory, currentAbs, packageName, parser)
		if err != nil {
			return nil, err
		}
		for key, returnType := range functionReturnTypes {
			record(key, returnType)
		}
	}

	results := make(map[string]string, len(candidates))
	for key, candidate := range candidates {
		if candidate.ambiguous || candidate.value == "" {
			continue
		}
		results[key] = candidate.value
	}
	return results, nil
}

func kotlinCollectFunctionReturnTypesFromDirectory(directory string, currentAbs string, packageName string, parser *tree_sitter.Parser) (map[string]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}

	results := make(map[string]string)
	for _, entry := range entries {
		path := filepath.Join(directory, entry.Name())
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), "TemporaryItems") {
				continue
			}
			functionReturnTypes, err := kotlinCollectFunctionReturnTypesFromDirectory(path, currentAbs, packageName, parser)
			if err != nil {
				return nil, err
			}
			for key, returnType := range functionReturnTypes {
				if _, ok := results[key]; ok {
					continue
				}
				results[key] = returnType
			}
			continue
		}
		if filepath.Ext(entry.Name()) != ".kt" {
			continue
		}
		if absPath, err := filepath.Abs(path); err == nil && absPath == currentAbs {
			continue
		}

		functionReturnTypes, err := kotlinCollectFunctionReturnTypesFromFile(path, packageName, parser)
		if err != nil {
			return nil, err
		}
		for key, returnType := range functionReturnTypes {
			if _, ok := results[key]; ok {
				continue
			}
			results[key] = returnType
		}
	}
	return results, nil
}

// kotlinCollectFunctionReturnTypesFromFile parses one sibling Kotlin file with
// tree-sitter and returns its function return-type map when the file shares the
// requested package. Extraction walks the AST so it agrees with the main parse.
func kotlinCollectFunctionReturnTypesFromFile(path string, packageName string, parser *tree_sitter.Parser) (map[string]string, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	if parser == nil {
		return nil, errNilParser
	}
	tree := parser.Parse(source, nil)
	if tree == nil {
		return nil, errNilTree
	}
	defer tree.Close()

	filePackageName := kotlinPackageNameFromTree(tree.RootNode(), source)
	if packageName != "" && filePackageName != packageName {
		return nil, nil
	}

	collector := &astWalker{source: source, packageName: filePackageName, functionReturnTypes: make(map[string]string)}
	collector.collectReturnTypesIn(tree.RootNode(), "")
	return collector.functionReturnTypes, nil
}

func kotlinQualifiedFunctionReturnKey(packageName string, key string) string {
	packageName = strings.TrimSpace(packageName)
	key = strings.TrimSpace(key)
	if packageName == "" || key == "" {
		return ""
	}
	return packageName + "::" + key
}

func kotlinStoreFunctionReturnType(functionReturnTypes map[string]string, packageName string, key string, returnType string) {
	key = strings.TrimSpace(key)
	returnType = strings.TrimSpace(returnType)
	if key == "" || returnType == "" {
		return
	}
	functionReturnTypes[key] = returnType
	if qualified := kotlinQualifiedFunctionReturnKey(packageName, key); qualified != "" {
		functionReturnTypes[qualified] = returnType
	}
}

func kotlinLookupFunctionReturnType(
	functionReturnTypes map[string]string,
	packageName string,
	currentClass string,
	name string,
) string {
	packageName = strings.TrimSpace(packageName)
	currentClass = strings.TrimSpace(currentClass)
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	if packageName != "" {
		if currentClass != "" {
			if returnType := strings.TrimSpace(functionReturnTypes[kotlinQualifiedFunctionReturnKey(packageName, currentClass+"."+name)]); returnType != "" {
				return returnType
			}
		}
		if returnType := strings.TrimSpace(functionReturnTypes[kotlinQualifiedFunctionReturnKey(packageName, name)]); returnType != "" {
			return returnType
		}
	}

	if currentClass != "" {
		if returnType := strings.TrimSpace(functionReturnTypes[currentClass+"."+name]); returnType != "" {
			return returnType
		}
	}
	return strings.TrimSpace(functionReturnTypes[name])
}
