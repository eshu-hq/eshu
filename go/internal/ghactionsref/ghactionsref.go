// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsref

import "strings"

// Parse splits a raw GitHub Actions reference value -- the string that
// follows a workflow's or step's `uses:` key, or a local reusable-workflow
// path -- into its repository slug, in-repo path, and @ref components.
//
// Recognized shapes:
//
//   - "owner/repo@ref" -> repo="owner/repo", ref="ref"
//   - "owner/repo/path/to/workflow.yml@ref" -> repo="owner/repo",
//     path="path/to/workflow.yml", ref="ref"
//   - "./.github/workflows/build.yml@ref" (local reusable workflow) ->
//     repo="", path=".github/workflows/build.yml", ref="ref"
//   - "owner/repo" (no @ segment) -> repo="owner/repo", ref="" (honest
//     absence -- Parse never fabricates a ref)
//
// This is the single ref-splitting implementation for Eshu's GitHub Actions
// evidence surfaces. It absorbs what were two independently maintained
// implementations of the same @-index logic:
// relationships.parseGitHubRefParts (go/internal/relationships/first_party_refs.go)
// and the query package's local `uses:` split helpers
// (go/internal/query/content_relationships_github_actions.go,
// go/internal/query/repository_workflow_artifacts.go). Both now delegate here
// so a future edit to the split rule cannot silently diverge between the
// reducer/graph-projection path and the query/read-model path.
func Parse(raw string) (repo string, path string, refValue string) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", ""
	}
	if at := strings.Index(trimmed, "@"); at >= 0 {
		refValue = strings.TrimSpace(trimmed[at+1:])
		trimmed = strings.TrimSpace(trimmed[:at])
	}
	trimmed = strings.TrimPrefix(trimmed, "./")
	trimmed = strings.TrimPrefix(trimmed, "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 {
		return "", trimmed, refValue
	}
	if parts[0] == ".github" {
		return "", trimmed, refValue
	}
	repo = strings.Join(parts[:2], "/")
	if len(parts) > 2 {
		path = strings.Join(parts[2:], "/")
	}
	return repo, path, refValue
}

// Pinned reports whether refValue is a full-length commit SHA: exactly 40
// hexadecimal characters (the SHA-1 object id GitHub uses today) or exactly
// 64 hexadecimal characters (a future SHA-256 object id). Any other value --
// a branch name, a tag, an abbreviated/short SHA, or an empty ref -- returns
// false.
//
// This is deliberately the ONLY property Pinned ever classifies. A branch
// name and a tag are statically indistinguishable from each other in a
// workflow file (both are just a ref string; resolving which one a name
// refers to requires calling GitHub, which Eshu's static extraction does
// not do), and a tag is mutable regardless of which one it is -- a repository
// owner can force-move a tag to point at a different commit at any time. Full
// commit SHA immutability is the one fact that can be proven from the ref
// string alone, so it is the one fact this signal asserts. An abbreviated SHA
// is intentionally NOT treated as pinned: it is short enough to collide, and
// treating it as safe would fabricate a safety guarantee the string does not
// actually prove.
func Pinned(refValue string) bool {
	trimmed := strings.TrimSpace(refValue)
	switch len(trimmed) {
	case 40, 64:
	default:
		return false
	}
	for _, r := range trimmed {
		if !isHexDigit(r) {
			return false
		}
	}
	return true
}

func isHexDigit(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}
