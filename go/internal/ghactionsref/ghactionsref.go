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

// ReusableWorkflowRepo returns the owner/repo slug for a GitHub Actions
// job-level `uses:` value that names a REMOTE reusable workflow --
// "owner/repo/.github/workflows/*.yml@ref". It returns "" for every other
// shape: a local (`./`-prefixed) reusable workflow, a bare action reference
// with no in-repo path segment at all, or a path whose third `/`-separated
// segment is not literally `.github` (a reusable workflow must live under
// `.github/workflows/`; GitHub does not allow any other location).
//
// This is issue #5526's consolidation of the two independently maintained
// implementations of this exact check:
// relationships.reusableWorkflowRepoRef (go/internal/relationships/github_actions_evidence.go)
// and query.githubActionsReusableWorkflowRepoRef
// (go/internal/query/content_relationships_github_actions.go). Both now
// delegate here, byte-identical to the implementation each used to contain.
//
// Deliberately does NOT strip a trailing `@ref` before computing the
// returned slug when the input has fewer than 3 `/`-separated segments
// (there is nothing to strip in that case -- see the `len(parts) < 3` early
// return). For a well-formed 3+-segment reusable-workflow reference the @ref
// always lives in the LAST segment, never in the owner or repo segment, so
// the returned slug is always ref-free; see ActionRepo's doc comment for the
// one shape (`owner/repo@ref`, no path) where THAT sibling function's
// ref-handling differs.
func ReusableWorkflowRepo(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if at := strings.Index(trimmed, "@"); at >= 0 {
		trimmed = trimmed[:at]
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) < 3 {
		return ""
	}
	if parts[0] == "." {
		return ""
	}
	if parts[2] != ".github" {
		return ""
	}
	return strings.Join(parts[:2], "/")
}

// ActionRepo returns the owner/repo slug embedded in a GitHub Actions
// step-level `uses:` value that names a marketplace or third-party action --
// excluding a Docker action (`docker://...`), the `actions/checkout` action
// (its target repository comes from a `with: repository:` input, a separate
// evidence path this package does not own), a local (`./`- or
// `.github/`-prefixed) action, and any reusable-workflow-shaped value
// (ReusableWorkflowRepo's shape, handled by that sibling function instead).
//
// ActionRepo intentionally performs NO ref-stripping of its own before
// building the returned slug: for the common two-segment "owner/repo@ref"
// shape, `strings.Split(trimmed, "/")` puts the entire "repo@ref" text in the
// second segment, and this function joins the first two segments verbatim --
// so the returned slug still carries the "@ref" suffix for that shape. This
// is issue #5526's consolidation of
// relationships.githubActionsActionRepoRef
// (go/internal/relationships/github_actions_evidence.go), preserved exactly
// as that function's pre-#5526 behavior, including this latent quirk: the
// evidence Details field it feeds ("action_repository") has carried a
// trailing "@ref" for a plain two-segment action reference since before this
// refactor, and #5526 is a behavior-preserving change, so the quirk is kept
// rather than silently fixed. The graph edge target itself is unaffected --
// catalogMatcher.match tokenizes on non-slug characters including "@", so the
// stray suffix does not change which repository the evidence resolves to.
// go/internal/query's githubActionsActionRepositoryRef wants a clean,
// ref-free slug instead; it gets one by re-splitting ActionRepo's result
// through Parse (Parse is idempotent on an already ref-free value and strips
// a still-attached "@ref" when one is present).
func ActionRepo(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.HasPrefix(trimmed, "docker://") {
		return ""
	}
	if strings.HasPrefix(trimmed, "actions/checkout@") {
		return ""
	}
	if strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, ".github/") {
		return ""
	}
	if ReusableWorkflowRepo(trimmed) != "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	if len(parts) < 2 || parts[0] == "." {
		return ""
	}
	return strings.Join(parts[:2], "/")
}

// IsWorkflowPath reports whether value is a GitHub Actions workflow file's
// canonical repository-relative path: exactly three `/`-separated segments
// shaped ".github/workflows/<name>.yml" or ".github/workflows/<name>.yaml",
// where <name> is non-empty. GitHub discovers workflow files only in the
// direct .github/workflows directory, so this deliberately rejects a nested
// workflow subdirectory (".github/workflows/team/ci.yml"), a path that merely
// contains the "workflows" segment as a substring
// ("examples/.github/workflows/ci.yml"), a non-YAML suffix, and a bare
// extension with no basename (".github/workflows/.yml") -- that last shape
// previously slipped through content/shape's local copy of this gate because
// it checked only the file extension, not that a non-empty name preceded it.
//
// This is the single exact-path gate for issue #5568's live GitHub Actions
// query surface: go/internal/content/shape (the content-entity identity
// gate, materialize.go's isDirectGitHubActionsWorkflowPath) and
// go/internal/query (the content-relationship classifier's workflow-path
// branch, content_relationships_github_actions.go's
// isGitHubActionsArtifactPath) both delegate here, so the "identical path
// contract" the content/shape README documents is literally the same code
// and cannot silently drift between the two packages again.
func IsWorkflowPath(value string) bool {
	parts := strings.Split(strings.TrimSpace(value), "/")
	if len(parts) != 3 || parts[0] != ".github" || parts[1] != "workflows" {
		return false
	}
	filename := parts[2]
	for _, extension := range []string{".yml", ".yaml"} {
		if strings.HasSuffix(filename, extension) && strings.TrimSuffix(filename, extension) != "" {
			return true
		}
	}
	return false
}

// LocalReusableWorkflowPath returns the in-repo workflow path for a LOCAL
// GitHub Actions reusable-workflow `uses:` value -- one that resolves inside
// the same repository, whether written with the conventional `./` prefix
// ("./.github/workflows/build.yml@ref") or without it
// (".github/workflows/build.yml@ref", which GitHub also accepts). Returns ""
// for any value whose (@ref-stripped, `./`-and-leading-`/`-stripped) form
// does not start with the literal segment ".github/workflows/" -- including
// a remote "owner/repo/.github/workflows/*.yml@ref" reusable-workflow
// reference, which ReusableWorkflowRepo handles instead.
//
// This is issue #5526's consolidation of the two byte-identical
// implementations of this check: relationships.githubActionsLocalReusableWorkflowPath
// (go/internal/relationships/github_actions_evidence.go) and
// query.githubActionsLocalReusableWorkflowPath
// (go/internal/query/repository_workflow_artifacts.go). Both now delegate
// here.
func LocalReusableWorkflowPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if at := strings.Index(trimmed, "@"); at >= 0 {
		trimmed = trimmed[:at]
	}
	trimmed = strings.TrimPrefix(trimmed, "./")
	trimmed = strings.TrimPrefix(trimmed, "/")
	if !strings.HasPrefix(trimmed, ".github/workflows/") {
		return ""
	}
	return trimmed
}
