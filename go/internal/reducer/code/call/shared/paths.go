// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// CallLanguage returns the resolved language for a call/edge: the explicit
// "lang" field when present, else a guess from the file extension.
func CallLanguage(call map[string]any, rawPath string, relativePath string) string {
	if language := strings.TrimSpace(payloadcore.AnyToString(call["lang"])); language != "" {
		return language
	}

	path := PreferredPath(rawPath, relativePath)
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

// HasQualifiedScope reports whether a call carries enough qualification
// (a dotted full_name, a class context, an inferred receiver type, or a
// Ruby class/module context) that a broad unqualified-name fallback would be
// unsafe.
func HasQualifiedScope(call map[string]any, language string) bool {
	if codeCallHasQualifiedFullName(payloadcore.AnyToString(call["full_name"])) {
		return true
	}
	if len(codeCallClassContexts(call)) > 0 {
		return true
	}
	if strings.TrimSpace(payloadcore.AnyToString(call["inferred_obj_type"])) != "" {
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

// PathKeys returns the deduplicated, slash-normalized path keys under which an
// entity at rawPath/relativePath may be indexed: the normalized rawPath and
// relativePath themselves, plus each one's base filename. Callers probe the
// index under every returned key because a call site may reference the file
// by either its full path or its bare filename. Empty inputs contribute no
// key.
func PathKeys(rawPath string, relativePath string) []string {
	keys := make([]string, 0, 4)
	appendKey := func(value string) {
		normalized := NormalizePath(value)
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

// NormalizePath cleans value and converts it to slash-separated form, or
// returns "" when value is blank.
func NormalizePath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(trimmed))
}

func codeCallPathLineKey(path string, line int) string {
	return fmt.Sprintf("%s#%d", path, line)
}

// PayloadInt returns the first value in values that is an int, int32, int64,
// float32, or float64, converted to int. It is meant for trying a preferred
// field followed by fallback fields (for example line_number then
// start_line). It returns 0 when no value matches a numeric type, so 0 is
// indistinguishable from an explicit zero and callers must guard with a
// separate presence check when that distinction matters.
func PayloadInt(values ...any) int {
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

// CopyOptionalField copies src[key] into dst[key] when present and non-nil.
func CopyOptionalField(dst map[string]any, src map[string]any, key string) {
	if value, ok := src[key]; ok && value != nil {
		dst[key] = value
	}
}
