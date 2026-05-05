package parser

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var kotlinPackagePattern = regexp.MustCompile(`^\s*package\s+([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*)\s*$`)

func kotlinCollectSiblingFunctionReturnTypes(repoRoot string, currentPath string, packageName string) (map[string]string, error) {
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
		functionReturnTypes, err := kotlinCollectFunctionReturnTypesFromDirectory(directory, currentAbs, packageName)
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

func kotlinCollectFunctionReturnTypesFromDirectory(directory string, currentAbs string, packageName string) (map[string]string, error) {
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
			functionReturnTypes, err := kotlinCollectFunctionReturnTypesFromDirectory(path, currentAbs, packageName)
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

		functionReturnTypes, err := kotlinCollectFunctionReturnTypesFromFile(path, packageName)
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

func kotlinCollectFunctionReturnTypesFromFile(path string, packageName string) (map[string]string, error) {
	source, err := readSource(path)
	if err != nil {
		return nil, err
	}
	filePackageName := kotlinFilePackage(string(source))
	if packageName != "" && filePackageName != packageName {
		return nil, nil
	}

	lines := strings.Split(string(source), "\n")
	braceDepth := 0
	stack := make([]scopedContext, 0)
	results := make(map[string]string)

	for _, rawLine := range lines {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			braceDepth += braceDelta(rawLine)
			stack = popCompletedScopes(stack, braceDepth)
			continue
		}

		if matches := kotlinClassPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			stack = append(stack, scopedContext{kind: "class", name: matches[1], braceDepth: braceDepth + max(1, strings.Count(rawLine, "{"))})
		}
		if matches := kotlinObjectPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			stack = append(stack, scopedContext{kind: "class", name: matches[1], braceDepth: braceDepth + max(1, strings.Count(rawLine, "{"))})
		}
		if matches := kotlinCompanionPattern.FindStringSubmatch(trimmed); len(matches) >= 1 {
			name := "Companion"
			if len(matches) == 2 && strings.TrimSpace(matches[1]) != "" {
				name = matches[1]
			}
			stack = append(stack, scopedContext{kind: "companion", name: name, braceDepth: braceDepth + max(1, strings.Count(rawLine, "{"))})
		}
		if matches := kotlinInterfacePattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			stack = append(stack, scopedContext{kind: "class", name: matches[1], braceDepth: braceDepth + max(1, strings.Count(rawLine, "{"))})
		}
		if matches := kotlinEnumPattern.FindStringSubmatch(trimmed); len(matches) == 2 {
			stack = append(stack, scopedContext{kind: "class", name: matches[1], braceDepth: braceDepth + max(1, strings.Count(rawLine, "{"))})
		}

		if matches := kotlinFunctionPattern.FindStringSubmatch(trimmed); len(matches) == 3 {
			receiverType, functionName, returnType := kotlinFunctionDeclarationReturnType(trimmed)
			if functionName != "" && returnType != "" {
				key := functionName
				if receiverType != "" {
					key = receiverType + "." + functionName
				} else if classContext := kotlinCurrentTypeScopeName(stack); classContext != "" {
					key = classContext + "." + functionName
				}
				kotlinStoreFunctionReturnType(results, filePackageName, key, returnType)
			}
			if strings.Contains(rawLine, "{") {
				stack = append(stack, scopedContext{
					kind:       "function",
					name:       matches[2],
					braceDepth: braceDepth + max(1, strings.Count(rawLine, "{")),
				})
			}
		}

		braceDepth += braceDelta(rawLine)
		stack = popCompletedScopes(stack, braceDepth)
	}

	return results, nil
}

func kotlinFilePackage(source string) string {
	for _, rawLine := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(rawLine)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		matches := kotlinPackagePattern.FindStringSubmatch(trimmed)
		if len(matches) == 2 {
			return strings.TrimSpace(matches[1])
		}
		break
	}
	return ""
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
