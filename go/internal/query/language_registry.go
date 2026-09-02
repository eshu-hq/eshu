// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "github.com/eshu-hq/eshu/go/internal/query/querycontract"

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
	return querycontract.CanonicalLanguage(language)
}

func normalizedLanguageVariants(language string) []string {
	return querycontract.NormalizedLanguageVariants(language)
}
