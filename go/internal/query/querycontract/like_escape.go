// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import "strings"

// likeEscapeReplacer escapes the three LIKE metacharacters so a caller
// pattern binds as a literal: backslash first (it is the escape itself),
// then the two wildcards. Postgres LIKE uses backslash as its default
// escape, so binds escaped with this helper need no ESCAPE clause.
var likeEscapeReplacer = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// EscapeLikeLiteral escapes a content-search pattern so it matches literally
// under LIKE/ILIKE (#10604, fix 05b). Every production content-search bind
// pre-escapes the caller's raw pattern with this helper exactly once, at the
// bind site: scoped file and entity statements, the unscoped walk's step and
// tail statements, and the legacy fallback builders. Classification
// (trigram presence) and scoring (rerank, rank walk) keep reading the raw
// pattern; only the SQL bind sees the escaped form.
func EscapeLikeLiteral(pattern string) string {
	return likeEscapeReplacer.Replace(pattern)
}
