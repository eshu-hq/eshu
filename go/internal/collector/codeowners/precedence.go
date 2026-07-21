// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

// candidatePathsByPrecedence lists the exact repo-relative CODEOWNERS
// locations GitHub honors, in the order GitHub resolves them: the first
// present path wins
// (https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-code-owners#codeowners-file-location).
// Paths are compared with forward slashes regardless of host OS.
var candidatePathsByPrecedence = []string{
	".github/CODEOWNERS",
	"CODEOWNERS",
	"docs/CODEOWNERS",
}

// CandidatePaths returns the three repo-relative CODEOWNERS locations GitHub
// honors, in precedence order (highest precedence first).
func CandidatePaths() []string {
	return append([]string(nil), candidatePathsByPrecedence...)
}

// IsCandidatePath reports whether relPath is exactly one of the three
// CODEOWNERS locations GitHub honors ([".github/CODEOWNERS", "CODEOWNERS",
// "docs/CODEOWNERS"]). Matching is an exact, case-sensitive, forward-slash
// comparison; it does not normalize case, backslashes, or path traversal, so
// callers must pass an already-cleaned, forward-slash, repo-relative path. On
// a match it returns relPath itself as the canonical source path.
func IsCandidatePath(relPath string) (string, bool) {
	for _, candidate := range candidatePathsByPrecedence {
		if relPath == candidate {
			return candidate, true
		}
	}
	return "", false
}

// ResolveWinner picks the single CODEOWNERS file GitHub would honor from a set
// of present candidate paths mapped to their bodies. GitHub resolves exactly
// one CODEOWNERS file per repository even when more than one candidate
// location exists, honoring ".github/CODEOWNERS" over root "CODEOWNERS" over
// "docs/CODEOWNERS". Candidate keys that are not one of the three recognized
// locations are ignored. ok is false when no recognized candidate is present.
func ResolveWinner(candidates map[string]string) (sourcePath string, body string, ok bool) {
	for _, candidate := range candidatePathsByPrecedence {
		if resolvedBody, present := candidates[candidate]; present {
			return candidate, resolvedBody, true
		}
	}
	return "", "", false
}
