// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package submodule

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

// ResolveRepoID resolves a submodule's raw ".gitmodules" URL to Eshu's
// canonical repo_id, mirroring the "never guess, empty means no durable
// link" discipline
// go/internal/collector/jira/linked_repository.go's linkedRepositoryID
// applies to PR/MR links: the empty string means "unresolved", never a
// guessed id a later phase would have to unwind.
//
// It returns a non-empty repo_id only when rawURL is an absolute,
// host-qualified git remote — https://, ssh://, git://, or the SCP
// "git@host:path" shorthand — that repositoryidentity.NormalizeRemoteURL can
// canonicalize into an "https://host/path" identity. Every other shape is
// treated as unresolved, most notably:
//
//   - git's own relative submodule URL forms ("../sibling.git", "./nested"),
//     which are meant to be resolved against the PARENT repository's own
//     remote, not canonicalized standalone. That relative-to-parent
//     resolution is out of scope for this phase (issue #5420 Phase 2a);
//     ResolveRepoID intentionally does not attempt it.
//   - a bare local filesystem path, an unparseable string, or any other
//     shape NormalizeRemoteURL cannot turn into a canonical host/path
//     identity.
//
// This check matters because repositoryidentity.CanonicalRepositoryID does
// NOT itself reject an uncanonicalizable remoteURL: when NormalizeRemoteURL
// cannot parse it into an "https://host/path" shape, CanonicalRepositoryID
// falls back to hashing the raw, unnormalized string as if it were a local
// path identity — silently producing a repo_id keyed off text that was never
// meant to be a durable identity (two spellings of the same relative URL
// would hash to two different, both-wrong ids). Gating on the "https://"
// prefix here, before ever calling CanonicalRepositoryID, is what keeps that
// guess from happening.
func ResolveRepoID(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}

	normalized := repositoryidentity.NormalizeRemoteURL(trimmed)
	if !strings.HasPrefix(normalized, "https://") {
		return ""
	}

	repoID, err := repositoryidentity.CanonicalRepositoryID(trimmed, "")
	if err != nil {
		return ""
	}
	return repoID
}
