// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import "strings"

// GitRepositoryScopePrefix is the scope id prefix of a git repository's
// ingestion scope. A default-branch scope is the prefix plus the repository
// id; a ref scope appends "@<ref>". It equals
// querycontract.GitRepositoryScopePrefix; recovery must not import the query
// layer, so a test in query/admin pins the two classifications together.
const GitRepositoryScopePrefix = "git-repository-scope:"

// IsGitDefaultBranchScope reports whether scopeID names a git default-branch
// repository scope: the only kind a per-repository reindex watermark can force
// to a full re-parse (#7797). It uses the semantics of
// querycontract.CanonicalRepositoryIDForScopeID: surrounding whitespace is
// trimmed, the prefix must match exactly, the remainder must be non-blank,
// and a remainder containing "@" (a ref scope) is rejected.
//
// The refinalize delta-active classification and the admin reindex route both
// call it, so a scope one accepts the other cannot refuse.
func IsGitDefaultBranchScope(scopeID string) bool {
	rest, ok := strings.CutPrefix(strings.TrimSpace(scopeID), GitRepositoryScopePrefix)
	if !ok {
		return false
	}
	rest = strings.TrimSpace(rest)
	return rest != "" && !strings.Contains(rest, "@")
}
