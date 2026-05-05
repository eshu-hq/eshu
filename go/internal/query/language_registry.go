package query

import "strings"

var languageAliases = map[string]string{
	"jsx": "javascript",
	"tsx": "typescript",
}

// supportedLanguages lists every language name accepted by language-query.
var supportedLanguages = map[string]bool{
	"c": true, "cpp": true, "csharp": true, "dart": true,
	"elixir": true, "go": true, "haskell": true, "java": true,
	"javascript": true, "jsx": true, "hcl": true, "kotlin": true,
	"perl": true, "php": true, "python": true, "ruby": true,
	"rust": true, "scala": true, "sql": true, "swift": true,
	"typescript": true, "tsx": true,
}

// languageFileExtensions maps language names to their common file extensions
// for more precise filtering.
var languageFileExtensions = map[string][]string{
	"c":          {".c", ".h"},
	"cpp":        {".cpp", ".cc", ".cxx", ".hpp", ".hxx", ".h"},
	"csharp":     {".cs"},
	"dart":       {".dart"},
	"elixir":     {".ex", ".exs"},
	"go":         {".go"},
	"haskell":    {".hs", ".lhs"},
	"java":       {".java"},
	"javascript": {".js", ".jsx", ".mjs", ".cjs"},
	"hcl":        {".hcl"},
	"kotlin":     {".kt", ".kts"},
	"perl":       {".pl", ".pm"},
	"php":        {".php"},
	"python":     {".py", ".pyi"},
	"ruby":       {".rb"},
	"rust":       {".rs"},
	"scala":      {".scala", ".sc"},
	"sql":        {".sql"},
	"swift":      {".swift"},
	"typescript": {".ts", ".tsx"},
}

func canonicalLanguage(language string) string {
	normalized := strings.ToLower(strings.TrimSpace(language))
	if canonical, ok := languageAliases[normalized]; ok {
		return canonical
	}
	return normalized
}

func normalizedLanguageVariants(language string) []string {
	switch canonicalLanguage(language) {
	case "javascript":
		return []string{"javascript", "jsx"}
	case "typescript":
		return []string{"typescript", "tsx"}
	default:
		return []string{canonicalLanguage(language)}
	}
}
