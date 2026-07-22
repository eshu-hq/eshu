// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Pin is the schema-version-1 typed payload for the "submodule.pin" fact
// kind (Contract System v1 §3.1, docs/internal/design/contract-system-v1.md).
//
// One Pin fact is one git submodule reference declared in a parent
// repository: a ".gitmodules" entry, a bare gitlink tree entry with no
// ".gitmodules" declaration, or both. It is the join between a parent
// repository and the submodule it embeds at one path, so the submodule
// dependency edge is queryable through the generic graph tools without a
// dedicated read surface (issue #5420 sets read_surface: none; the git
// collector emits this fact and the reducer projects it into a
// Repository-[:PINS_SUBMODULE]->Repository graph edge).
//
// ParentRepoID and SubmodulePath are the only fields a collector always has
// once it has found a submodule reference at all, so they are the required
// join identity here. SubmoduleURL, ResolvedRepoID, and PinnedSHA are each
// optional because a valid, non-dangling observation can be missing any one
// of them: a ".gitmodules" entry with no corresponding gitlink has a URL but
// no PinnedSHA; a gitlink with no ".gitmodules" entry (a submodule declared
// only as a tree gitlink) has a PinnedSHA but no SubmoduleURL; and
// ResolvedRepoID is nil whenever the submodule's URL cannot be resolved to a
// canonical repo_id through repositoryidentity (unresolved, ambiguous, or
// dangling). The collector emits this fact only when ParentRepoID and
// SubmodulePath are present AND at least one of SubmoduleURL or PinnedSHA is
// known; that emission constraint belongs to the collector, not this
// schema, so the schema itself only requires the join identity.
type Pin struct {
	// CollectorInstanceID identifies the collector run instance that
	// observed this submodule pin. Optional: it is emission metadata, not
	// part of the join identity a consumer keys off.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// ParentRepoID is the canonical repo_id of the repository whose tree
	// contains the ".gitmodules" entry and/or the gitlink for this
	// submodule. Required: half of the join identity a submodule-edge
	// consumer keys off.
	ParentRepoID string `json:"parent_repo_id"`

	// SubmodulePath is the path of the submodule within ParentRepoID's tree,
	// exactly as it appears in ".gitmodules" ("path = ...") or as the
	// gitlink tree entry's path. Required: the other half of the join
	// identity, distinguishing multiple submodules in the same parent repo.
	SubmodulePath string `json:"submodule_path"`

	// SubmoduleURL is the raw "url = ..." value from the parent's
	// ".gitmodules" file for this path. Optional: nil when a gitlink tree
	// entry exists for this path with no corresponding ".gitmodules" entry
	// (a submodule declared only as a bare tree gitlink).
	SubmoduleURL *string `json:"submodule_url,omitempty"`

	// ResolvedRepoID is the canonical repo_id SubmoduleURL resolves to via
	// repositoryidentity. Optional: nil whenever the URL cannot be resolved
	// to exactly one known repository — unresolved (no matching repo),
	// ambiguous (more than one candidate), or dangling (resolution attempted
	// but the target repo is not indexed).
	ResolvedRepoID *string `json:"resolved_repo_id,omitempty"`

	// PinnedSHA is the commit SHA the parent tree's gitlink entry records
	// for this submodule path. Optional: nil when the submodule is declared
	// in ".gitmodules" but the working tree carries no gitlink for it (for
	// example a ".gitmodules" entry with no corresponding tree entry).
	PinnedSHA *string `json:"pinned_sha,omitempty"`
}
