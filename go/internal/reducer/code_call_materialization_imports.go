package reducer

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func collectCodeCallRepositoryImports(
	envelopes []facts.Envelope,
) map[string]map[string][]string {
	repositoryImports := make(map[string]map[string][]string)
	for _, env := range envelopes {
		if env.FactKind != "repository" {
			continue
		}
		repositoryID := payloadStr(env.Payload, "repo_id")
		if repositoryID == "" {
			repositoryID = payloadStr(env.Payload, "graph_id")
		}
		if repositoryID == "" {
			continue
		}
		imports, ok := env.Payload["imports_map"]
		if !ok || imports == nil {
			continue
		}
		normalized := codeCallNormalizeRepositoryImports(imports)
		if len(normalized) == 0 {
			continue
		}
		repositoryImports[repositoryID] = normalized
	}
	return repositoryImports
}

func codeCallNormalizeRepositoryImports(value any) map[string][]string {
	result := make(map[string][]string)

	appendPath := func(name string, path string) {
		name = strings.TrimSpace(name)
		path = normalizeCodeCallPath(path)
		if name == "" || path == "" {
			return
		}
		for _, existing := range result[name] {
			if existing == path {
				return
			}
		}
		result[name] = append(result[name], path)
	}

	switch typed := value.(type) {
	case map[string][]string:
		for name, paths := range typed {
			for _, path := range paths {
				appendPath(name, path)
			}
		}
	case map[string]any:
		for name, rawPaths := range typed {
			switch paths := rawPaths.(type) {
			case []string:
				for _, path := range paths {
					appendPath(name, path)
				}
			case []any:
				for _, rawPath := range paths {
					appendPath(name, anyToString(rawPath))
				}
			}
		}
	}

	return result
}

func resolveGenericCallee(
	index codeEntityIndex,
	repositoryID string,
	repositoryImports map[string][]string,
	reexportIndex codeCallReexportIndex,
	rawPath string,
	relativePath string,
	fileData map[string]any,
	call map[string]any,
) (string, string) {
	callLine := codeCallInt(call["line_number"], call["ref_line"])
	if entityID := resolveSameFileScopedCalleeEntityID(index, rawPath, relativePath, call, callLine); entityID != "" {
		return entityID, codeCallPreferredPath(rawPath, relativePath)
	}
	if entityID := resolveDynamicJavaScriptCalleeEntityID(index, rawPath, relativePath, fileData, call); entityID != "" {
		return entityID, codeCallPreferredPath(rawPath, relativePath)
	}
	if entityID := resolveSameFileCalleeEntityID(index, rawPath, relativePath, call); entityID != "" {
		return entityID, codeCallPreferredPath(rawPath, relativePath)
	}

	language := codeCallLanguage(call, rawPath, relativePath)
	if language == "go" && !codeCallHasQualifiedScope(call, language) {
		if entityID := resolveGoSameDirectoryCalleeEntityID(index, repositoryID, rawPath, relativePath, call, language); entityID != "" {
			return entityID, index.entityFileByID[entityID]
		}
	}
	for _, name := range codeCallExactCandidateNames(call, language) {
		if entityID := index.uniqueNameByRepo[repositoryID][name]; entityID != "" {
			return entityID, index.entityFileByID[entityID]
		}
	}
	if !codeCallHasQualifiedScope(call, language) {
		for _, name := range codeCallBroadCandidateNames(call, language) {
			if entityID := index.uniqueNameByRepo[repositoryID][name]; entityID != "" {
				return entityID, index.entityFileByID[entityID]
			}
		}
	}

	return resolveImportedCrossFileCallee(
		index,
		repositoryImports,
		reexportIndex,
		repositoryID,
		rawPath,
		relativePath,
		fileData,
		call,
	)
}

func resolveGoSameDirectoryCalleeEntityID(
	index codeEntityIndex,
	repositoryID string,
	rawPath string,
	relativePath string,
	call map[string]any,
	language string,
) string {
	dir := codeCallDirectoryKey(codeCallPreferredPath(rawPath, relativePath))
	if repositoryID == "" || dir == "" {
		return ""
	}
	for _, name := range codeCallExactCandidateNames(call, language) {
		if entityID := index.uniqueNameByRepoDir[repositoryID][dir][name]; entityID != "" {
			return entityID
		}
	}
	for _, name := range codeCallBroadCandidateNames(call, language) {
		if entityID := index.uniqueNameByRepoDir[repositoryID][dir][name]; entityID != "" {
			return entityID
		}
	}
	return ""
}

func resolveImportedCrossFileCallee(
	index codeEntityIndex,
	repositoryImports map[string][]string,
	reexportIndex codeCallReexportIndex,
	repositoryID string,
	rawPath string,
	relativePath string,
	fileData map[string]any,
	call map[string]any,
) (string, string) {
	importEntries := mapSlice(fileData["imports"])
	for _, target := range codeCallImportedTargets(importEntries, call) {
		language := codeCallLanguage(call, rawPath, relativePath)
		if len(repositoryImports) > 0 {
			paths := repositoryImports[target.symbolName]
			matchedPath := codeCallMatchImportedPath(
				rawPath,
				relativePath,
				target.importSource,
				language,
				paths,
			)
			if matchedPath != "" {
				if entityID := index.uniqueNameByPath[matchedPath][target.symbolName]; entityID != "" {
					return entityID, index.entityFileByID[entityID]
				}
			}
		}
		if entityID, calleeFile := resolveReexportedCrossFileCallee(
			index,
			reexportIndex,
			repositoryID,
			rawPath,
			relativePath,
			language,
			target,
		); entityID != "" {
			return entityID, calleeFile
		}
	}

	return "", ""
}

type codeCallImportedTarget struct {
	symbolName   string
	importSource string
}

func codeCallImportedTargets(
	importEntries []map[string]any,
	call map[string]any,
) []codeCallImportedTarget {
	callName := strings.TrimSpace(anyToString(call["name"]))
	callFullName := strings.TrimSpace(anyToString(call["full_name"]))
	if callName == "" {
		return nil
	}

	targets := make([]codeCallImportedTarget, 0, 2)
	appendTarget := func(symbolName string, importSource string) {
		symbolName = strings.TrimSpace(symbolName)
		importSource = strings.TrimSpace(importSource)
		if symbolName == "" || importSource == "" {
			return
		}
		for _, existing := range targets {
			if existing.symbolName == symbolName && existing.importSource == importSource {
				return
			}
		}
		targets = append(targets, codeCallImportedTarget{
			symbolName:   symbolName,
			importSource: importSource,
		})
	}

	if source := codeCallDynamicImportSource(callFullName); source != "" {
		appendTarget(callName, source)
	}

	for _, entry := range importEntries {
		entryName := strings.TrimSpace(anyToString(entry["name"]))
		entryAlias := strings.TrimSpace(anyToString(entry["alias"]))
		entrySource := strings.TrimSpace(anyToString(entry["source"]))
		entryType := strings.TrimSpace(anyToString(entry["import_type"]))
		if entrySource == "" {
			continue
		}

		localName := entryName
		if entryAlias != "" {
			localName = entryAlias
		}
		if localName == callName && entryName != "*" {
			for _, source := range codeCallImportEntrySources(entry) {
				appendTarget(entryName, source)
			}
		}

		if localName != "" && entryName == localName && strings.HasPrefix(callFullName, localName+".") {
			qualifiedName := entryName + strings.TrimPrefix(callFullName, localName)
			for _, source := range codeCallImportEntrySources(entry) {
				appendTarget(qualifiedName, source)
			}
		}

		if (entryName == "*" || entryType == "import") && entryAlias != "" {
			prefix := entryAlias + "."
			if strings.HasPrefix(callFullName, prefix) {
				for _, source := range codeCallImportEntrySources(entry) {
					appendTarget(callName, source)
				}
			}
		}
	}

	return targets
}

func codeCallDynamicImportSource(fullName string) string {
	fullName = strings.TrimSpace(fullName)
	if fullName == "" || !strings.Contains(fullName, "import(") {
		return ""
	}
	start := strings.Index(fullName, "import(")
	if start < 0 {
		return ""
	}
	remainder := fullName[start+len("import("):]
	remainder = strings.TrimLeft(remainder, " \t\n\r")
	if remainder == "" {
		return ""
	}
	quote := remainder[0]
	if quote != '\'' && quote != '"' {
		return ""
	}
	end := strings.IndexByte(remainder[1:], quote)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(remainder[1 : 1+end])
}

func codeCallImportEntrySources(entry map[string]any) []string {
	sources := make([]string, 0, 2)
	appendSource := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range sources {
			if existing == value {
				return
			}
		}
		sources = append(sources, value)
	}
	appendSource(anyToString(entry["resolved_source"]))
	appendSource(anyToString(entry["source"]))
	return sources
}

func codeCallMatchImportedPath(
	rawPath string,
	relativePath string,
	importSource string,
	language string,
	candidatePaths []string,
) string {
	for _, expectedPath := range codeCallImportSourceCandidates(
		rawPath,
		relativePath,
		importSource,
		language,
	) {
		for _, candidatePath := range candidatePaths {
			if normalizeCodeCallPath(candidatePath) == expectedPath {
				return expectedPath
			}
		}
	}
	return ""
}

func codeCallImportSourceCandidates(
	rawPath string,
	relativePath string,
	importSource string,
	language string,
) []string {
	importSource = strings.TrimSpace(importSource)
	candidates := make([]string, 0, 12)
	appendCandidate := func(value string) {
		normalized := normalizeCodeCallPath(value)
		if normalized == "" {
			return
		}
		for _, existing := range candidates {
			if existing == normalized {
				return
			}
		}
		candidates = append(candidates, normalized)
	}

	appendCandidate(importSource)

	callerPath := normalizeCodeCallPath(rawPath)
	if callerPath == "" {
		callerPath = normalizeCodeCallPath(relativePath)
	}

	repositoryRoot := codeCallRepositoryRoot(rawPath, relativePath)
	if repositoryRoot != "" && importSource != "" && !strings.HasPrefix(importSource, ".") {
		appendCandidate(filepath.Join(repositoryRoot, importSource))
	}

	if strings.HasPrefix(importSource, ".") && callerPath != "" {
		basePath := normalizeCodeCallPath(filepath.Join(filepath.Dir(callerPath), importSource))
		appendCandidate(basePath)
		for _, ext := range []string{".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts"} {
			appendCandidate(basePath + ext)
			appendCandidate(filepath.Join(basePath, "index"+ext))
		}
		if withoutJSExt := codeCallTrimJavaScriptRuntimeExtension(basePath); withoutJSExt != "" {
			for _, ext := range []string{".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs"} {
				appendCandidate(withoutJSExt + ext)
				appendCandidate(filepath.Join(withoutJSExt, "index"+ext))
			}
		}
		if language == "python" {
			appendCandidate(basePath + ".py")
			appendCandidate(filepath.Join(basePath, "__init__.py"))
		}
	}

	if language == "python" {
		for _, candidate := range codeCallPythonModuleCandidates(
			repositoryRoot,
			importSource,
		) {
			appendCandidate(candidate)
		}
	}

	return candidates
}

func codeCallTrimJavaScriptRuntimeExtension(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".js", ".jsx", ".mjs", ".cjs":
		return strings.TrimSuffix(path, filepath.Ext(path))
	default:
		return ""
	}
}

func codeCallRepositoryRoot(rawPath string, relativePath string) string {
	callerPath := normalizeCodeCallPath(rawPath)
	repositoryPath := normalizeCodeCallPath(relativePath)
	if callerPath == "" || repositoryPath == "" {
		return ""
	}
	if !strings.HasSuffix(callerPath, repositoryPath) {
		return ""
	}
	root := strings.TrimSuffix(callerPath, repositoryPath)
	return normalizeCodeCallPath(root)
}

func codeCallPythonModuleCandidates(
	repositoryRoot string,
	importSource string,
) []string {
	importSource = strings.TrimSpace(importSource)
	if repositoryRoot == "" || importSource == "" || strings.Contains(importSource, "/") {
		return nil
	}

	leadingDots := 0
	for leadingDots < len(importSource) && importSource[leadingDots] == '.' {
		leadingDots++
	}
	modulePath := strings.TrimLeft(importSource, ".")
	if modulePath == "" || !strings.Contains(modulePath, ".") {
		return nil
	}

	resolved := strings.ReplaceAll(modulePath, ".", string(filepath.Separator))
	basePath := filepath.Join(repositoryRoot, resolved)
	return []string{
		basePath + ".py",
		filepath.Join(basePath, "__init__.py"),
	}
}
