// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recovery

import "strings"

// GitRepositoryScopePrefix is the scope id prefix of a git repository's
// ingestion scope. A default-branch scope is the prefix plus the repository
// id; a ref scope appends "@<ref>". This is the single source of the literal:
// querycontract.GitRepositoryScopePrefix is defined as this constant, so the
// prefix cannot drift. The classification logic still exists in both packages,
// and a test in query/admin pins the two together on every edge case.
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
//
// It classifies a whitespace-padded id as a default-branch scope, but the
// callers write the reindex row with the id unchanged, which the collector
// would never match. That case is theoretical: the git collector builds the
// scope id as the prefix plus repositoryidentity.CanonicalRepositoryID, a
// "repository:r_<8-hex>" hash that cannot carry whitespace.
func IsGitDefaultBranchScope(scopeID string) bool {
	rest, ok := strings.CutPrefix(strings.TrimSpace(scopeID), GitRepositoryScopePrefix)
	if !ok {
		return false
	}
	rest = strings.TrimSpace(rest)
	return rest != "" && !strings.Contains(rest, "@")
}
