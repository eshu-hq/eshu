// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"fmt"
	"path/filepath"
	"strings"
)

func codeCallLanguage(call map[string]any, rawPath string, relativePath string) string {
	if language := strings.TrimSpace(anyToString(call["lang"])); language != "" {
		return language
	}

	path := codeCallPreferredPath(rawPath, relativePath)
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".php":
		return "php"
	case ".swift":
		return "swift"
	case ".rb":
		return "ruby"
	case ".ex", ".exs":
		return "elixir"
	default:
		return ""
	}
}

func codeCallHasQualifiedScope(call map[string]any, language string) bool {
	if codeCallHasQualifiedFullName(anyToString(call["full_name"])) {
		return true
	}
	if len(codeCallClassContexts(call)) > 0 {
		return true
	}
	if strings.TrimSpace(anyToString(call["inferred_obj_type"])) != "" {
		return true
	}
	if language != "ruby" {
		return false
	}
	contextName := codeCallContextName(call["context"])
	contextType := codeCallContextType(call)
	return contextName != "" && (contextType == "class" || contextType == "module")
}

func codeCallHasQualifiedFullName(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	return strings.ContainsAny(trimmed, ".:#/\\")
}

func codeCallTrailingSegments(value string, count int) string {
	if count <= 0 {
		return ""
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	cutset := func(r rune) bool {
		switch r {
		case '.', ':', '#', '/', '\\':
			return true
		default:
			return false
		}
	}
	parts := strings.FieldsFunc(trimmed, cutset)
	if len(parts) < count {
		return ""
	}
	return strings.Join(parts[len(parts)-count:], ".")
}

func codeCallPathKeys(rawPath string, relativePath string) []string {
	keys := make([]string, 0, 4)
	appendKey := func(value string) {
		normalized := normalizeCodeCallPath(value)
		if normalized == "" {
			return
		}
		for _, existing := range keys {
			if existing == normalized {
				return
			}
		}
		keys = append(keys, normalized)
	}

	appendKey(rawPath)
	appendKey(relativePath)
	if rawPath != "" {
		appendKey(filepath.Base(rawPath))
	}
	if relativePath != "" {
		appendKey(filepath.Base(relativePath))
	}
	return keys
}

func normalizeCodeCallPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(trimmed))
}

func codeCallPathLineKey(path string, line int) string {
	return fmt.Sprintf("%s#%d", path, line)
}

func mapSlice(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			asMap, ok := item.(map[string]any)
			if ok {
				result = append(result, asMap)
			}
		}
		return result
	default:
		return nil
	}
}

func codeCallInt(values ...any) int {
	for _, value := range values {
		switch typed := value.(type) {
		case int:
			return typed
		case int32:
			return int(typed)
		case int64:
			return int(typed)
		case float32:
			return int(typed)
		case float64:
			return int(typed)
		}
	}
	return 0
}

func copyOptionalCodeCallField(dst map[string]any, src map[string]any, key string) {
	if value, ok := src[key]; ok && value != nil {
		dst[key] = value
	}
}
