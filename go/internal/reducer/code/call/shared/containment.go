// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

// FileKeys returns the two keys that name a file's own identity inside one
// repository: the normalized full (checkout) path and the normalized
// repo-relative path. relativeKey is "" when it equals fullKey. When rawPath is
// blank the relative path takes the fullKey slot and relativeKey is "", and
// both are "" only when both inputs are blank. Unlike [PathKeys], FileKeys never
// returns a bare file name, so a lookup keyed by it cannot reach a same-named
// file in another directory or another repository (#7640).
func FileKeys(rawPath string, relativePath string) (fullKey string, relativeKey string) {
	fullKey = NormalizePath(rawPath)
	relativeKey = NormalizePath(relativePath)
	if relativeKey == fullKey {
		relativeKey = ""
	}
	if fullKey == "" {
		return relativeKey, ""
	}
	return fullKey, relativeKey
}

// addFileSpan records span for one file under its repository and its own
// file keys only (see [FileKeys]).
func addFileSpan(
	byFile map[string]map[string][]FunctionSpan,
	repositoryID string,
	fullKey string,
	relativeKey string,
	span FunctionSpan,
) {
	files := byFile[repositoryID]
	if files == nil {
		files = make(map[string][]FunctionSpan)
		byFile[repositoryID] = files
	}
	for _, key := range [2]string{fullKey, relativeKey} {
		if key != "" {
			files[key] = append(files[key], span)
		}
	}
}

// ResolveContainingEntityID returns the narrowest function or type span that
// contains a parser reference line, so a call attaches to the executable body
// that owns it.
//
// The lookup uses only the call file's own identity: (repositoryID,
// normalized full path) and (repositoryID, normalized relative path). It never
// consults a bare file name or another repository's path, so a top-level call
// with no enclosing span in its own file resolves to "" instead of borrowing a
// span from a same-named file elsewhere (#7640).
func ResolveContainingEntityID(
	index EntityIndex,
	repositoryID string,
	rawPath string,
	relativePath string,
	line int,
) string {
	files := index.containersByFile[repositoryID]
	if len(files) == 0 {
		return ""
	}
	fullKey, relativeKey := FileKeys(rawPath, relativePath)
	for _, fileKey := range [2]string{fullKey, relativeKey} {
		if fileKey == "" {
			continue
		}
		if entityID := NarrowestContainingSpan(files[fileKey], line).EntityID; entityID != "" {
			return entityID
		}
	}
	return ""
}

// NarrowestContainingSpan returns the narrowest span among spans whose line
// range includes line, or the zero FunctionSpan when none does. Ties keep the
// first span in slice order.
func NarrowestContainingSpan(spans []FunctionSpan, line int) FunctionSpan {
	var best FunctionSpan
	for _, span := range spans {
		if line < span.StartLine || line > span.EndLine {
			continue
		}
		if best.EntityID == "" || span.EndLine-span.StartLine < best.EndLine-best.StartLine {
			best = span
		}
	}
	return best
}
