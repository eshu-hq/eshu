package python

import (
	"os"
	"path/filepath"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func pythonImportEntries(
	path string,
	node *tree_sitter.Node,
	source []byte,
) []map[string]any {
	if node == nil {
		return nil
	}

	statement := strings.Join(strings.Fields(strings.TrimSpace(nodeText(node, source))), " ")
	if statement == "" {
		return nil
	}

	appendEntry := func(entries []map[string]any, name string, alias string, importSource string, importType string) []map[string]any {
		name = strings.TrimSpace(name)
		alias = strings.TrimSpace(alias)
		importSource = strings.TrimSpace(importSource)
		if name == "" || importSource == "" {
			return entries
		}

		entry := map[string]any{
			"name":             name,
			"source":           importSource,
			"full_import_name": statement,
			"import_type":      importType,
			"line_number":      nodeLine(node),
			"lang":             "python",
		}
		if alias != "" {
			entry["alias"] = alias
		}
		return append(entries, entry)
	}

	switch {
	case strings.HasPrefix(statement, "import "):
		entries := make([]map[string]any, 0)
		for _, clause := range pythonSplitImportClauses(strings.TrimSpace(strings.TrimPrefix(statement, "import "))) {
			modulePath, alias := pythonSplitImportAlias(clause)
			if modulePath == "" {
				continue
			}
			if alias == "" {
				alias = pythonImportLocalAlias(modulePath)
			}
			importSource := pythonResolvedImportSource(path, modulePath, "")
			if importSource == "" {
				importSource = modulePath
			}
			entries = appendEntry(entries, modulePath, alias, importSource, "import")
		}
		return entries
	case strings.HasPrefix(statement, "from "):
		rest := strings.TrimSpace(strings.TrimPrefix(statement, "from "))
		importIndex := strings.Index(rest, " import ")
		if importIndex == -1 {
			return nil
		}
		modulePath := strings.TrimSpace(rest[:importIndex])
		importClause := strings.TrimSpace(rest[importIndex+len(" import "):])
		importClause = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(importClause, "("), ")"))
		entries := make([]map[string]any, 0)
		for _, clause := range pythonSplitImportClauses(importClause) {
			if strings.TrimSpace(clause) == "*" {
				importSource := pythonResolvedImportSource(path, modulePath, "")
				if importSource == "" {
					importSource = modulePath
				}
				entries = appendEntry(entries, "*", "", importSource, "from")
				continue
			}
			name, alias := pythonSplitImportAlias(clause)
			if name == "" {
				continue
			}
			importSource := pythonResolvedImportSource(path, modulePath, name)
			if importSource == "" {
				importSource = modulePath
			}
			entries = appendEntry(entries, name, alias, importSource, "from")
		}
		return entries
	default:
		return nil
	}
}

func pythonSplitImportClauses(importClause string) []string {
	importClause = strings.TrimSpace(importClause)
	if importClause == "" {
		return nil
	}

	clauses := make([]string, 0)
	start := 0
	depth := 0
	for index, r := range importClause {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				if clause := strings.TrimSpace(importClause[start:index]); clause != "" {
					clauses = append(clauses, clause)
				}
				start = index + 1
			}
		}
	}
	if clause := strings.TrimSpace(importClause[start:]); clause != "" {
		clauses = append(clauses, clause)
	}
	return clauses
}

func pythonSplitImportAlias(clause string) (string, string) {
	clause = strings.TrimSpace(clause)
	if clause == "" {
		return "", ""
	}
	if left, right, ok := strings.Cut(clause, " as "); ok {
		return strings.TrimSpace(left), strings.TrimSpace(right)
	}
	return clause, ""
}

func pythonImportLocalAlias(modulePath string) string {
	modulePath = strings.TrimSpace(modulePath)
	modulePath = strings.TrimLeft(modulePath, ".")
	modulePath = strings.Trim(modulePath, "/")
	if modulePath == "" {
		return ""
	}
	if strings.Contains(modulePath, ".") {
		return strings.Split(modulePath, ".")[0]
	}
	return modulePath
}

func pythonResolvedImportSource(path string, modulePath string, importedName string) string {
	modulePath = strings.TrimSpace(modulePath)
	importedName = strings.TrimSpace(importedName)
	if modulePath == "" {
		return ""
	}

	if strings.HasPrefix(modulePath, ".") {
		if resolved := pythonResolvedRelativeImportSource(path, modulePath, importedName); resolved != "" {
			return resolved
		}
		return pythonRelativeImportFallback(modulePath, importedName)
	}

	if resolved := pythonResolvedAbsoluteImportSource(path, modulePath); resolved != "" {
		return resolved
	}
	return modulePath
}

func pythonResolvedRelativeImportSource(path string, modulePath string, importedName string) string {
	currentDir := filepath.Dir(path)
	if currentDir == "" {
		return ""
	}

	dotCount := 0
	for dotCount < len(modulePath) && modulePath[dotCount] == '.' {
		dotCount++
	}
	remainder := strings.TrimLeft(modulePath, ".")
	if remainder == "" {
		remainder = importedName
	}
	if remainder == "" {
		return ""
	}

	baseDir := currentDir
	for index := 1; index < dotCount; index++ {
		baseDir = filepath.Dir(baseDir)
	}

	if resolved := pythonResolveImportCandidate(baseDir, remainder); resolved != "" {
		return pythonRelativeImportSource(path, resolved)
	}
	return ""
}

func pythonResolvedAbsoluteImportSource(path string, modulePath string) string {
	currentDir := filepath.Dir(path)
	if currentDir == "" {
		return ""
	}

	moduleSegments := strings.ReplaceAll(modulePath, ".", string(filepath.Separator))
	for dir := currentDir; ; dir = filepath.Dir(dir) {
		if resolved := pythonResolveImportCandidate(dir, moduleSegments); resolved != "" {
			return pythonRelativeImportSource(path, resolved)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
	}
}

func pythonResolveImportCandidate(baseDir string, moduleSegments string) string {
	moduleSegments = strings.TrimSpace(moduleSegments)
	if baseDir == "" || moduleSegments == "" {
		return ""
	}

	candidateBase := filepath.Join(baseDir, filepath.FromSlash(moduleSegments))
	candidates := []string{
		candidateBase + ".py",
		filepath.Join(candidateBase, "__init__.py"),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func pythonRelativeImportSource(path string, resolvedPath string) string {
	currentDir := filepath.Dir(path)
	if currentDir == "" || resolvedPath == "" {
		return ""
	}

	relativePath, err := filepath.Rel(currentDir, resolvedPath)
	if err != nil {
		return ""
	}
	relativePath = strings.TrimSuffix(relativePath, filepath.Ext(relativePath))
	relativePath = filepath.ToSlash(relativePath)
	if relativePath == "" || relativePath == "." {
		return "./"
	}
	if strings.HasPrefix(relativePath, ".") {
		return relativePath
	}
	return "./" + relativePath
}

func pythonRelativeImportFallback(modulePath string, importedName string) string {
	dotCount := 0
	for dotCount < len(modulePath) && modulePath[dotCount] == '.' {
		dotCount++
	}
	remainder := strings.TrimLeft(modulePath, ".")
	if remainder == "" {
		remainder = importedName
	}
	if remainder == "" {
		return ""
	}

	remainder = strings.TrimPrefix(filepath.ToSlash(remainder), "/")
	if dotCount <= 1 {
		return "./" + remainder
	}
	return strings.Repeat("../", dotCount-1) + remainder
}
