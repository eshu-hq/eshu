// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadcore

import (
	"path"
	"strings"
)

// DeltaPayloadBool returns payload[key] when it is a real bool, and false
// otherwise. Unlike PayloadBool it does NOT accept the strings "true"/"false":
// the delta-generation markers it reads are emitted as JSON booleans, and
// accepting a string here would let a producer's stray "false" read as true
// through a lenient coercion. Absent, wrong-typed and false all mean "not on a
// delta generation", which is the safe reading -- a repository wrongly treated
// as full re-emits every file, while one wrongly treated as delta retracts only
// the changed files and silently keeps stale edges.
func DeltaPayloadBool(payload map[string]any, key string) bool {
	if len(payload) == 0 {
		return false
	}
	value, ok := payload[key]
	if !ok {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}

// QualifyDeltaPath joins a repository's checkout path with one collector-
// reported changed relative path, returning the repo-qualified file path the
// delta retract scopes on.
//
// It returns "" -- meaning "this path cannot be qualified, drop it" -- when
// either side is empty or when the relative path escapes the repository:
// absolute, "." , ".." or anything cleaning to a "../" prefix. Dropping is the
// only safe answer, because a path that escaped would scope a DELETE at files
// outside the repository the refresh intent owns.
func QualifyDeltaPath(repoPath string, relativePath string) string {
	if repoPath == "" || relativePath == "" {
		return ""
	}
	cleaned := path.Clean(relativePath)
	if cleaned == "" || cleaned == "." || cleaned == ".." ||
		path.IsAbs(cleaned) || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return path.Join(repoPath, cleaned)
}
