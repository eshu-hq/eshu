// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// Closed ref-kind values for BranchObservation.RefKind: the git object class
// the tracked ref names. A tag is indexed like a branch (immutable-ish, so
// cacheable); a commit is a detached SHA needing a targeted fetch.
const (
	// RefKindBranch names a branch ref.
	RefKindBranch = "branch"
	// RefKindTag names a tag ref.
	RefKindTag = "tag"
	// RefKindCommit names a bare commit SHA (detached).
	RefKindCommit = "commit"
)

// Closed resolvability values for BranchObservation.Resolvability: whether
// the tracked ref resolves to a concrete commit. Anything the pipeline
// cannot resolve to a commit (a templated variable, an unresolved
// generator, a semver range) is surfaced as unresolved — never guessed,
// never fallen back to the default branch.
const (
	// ResolvabilityPinned names a ref that resolves to one immutable commit.
	ResolvabilityPinned = "pinned"
	// ResolvabilityMobile names a ref that resolves today but moves (for
	// example a branch tip or a mutable tag).
	ResolvabilityMobile = "mobile"
	// ResolvabilityRange names a ref expression matching a set (for example
	// a semver range) rather than one commit.
	ResolvabilityRange = "range"
	// ResolvabilityUnresolved names a ref the pipeline could not resolve to
	// a concrete commit.
	ResolvabilityUnresolved = "unresolved"
)

// BranchObservation is the schema-version-1 typed payload for the
// "ref_overlay.branch_observation" fact kind (Contract System v1 §3.1,
// docs/internal/design/contract-system-v1.md, issue #5393).
//
// One BranchObservation fact is one deployment-referenced non-default ref:
// the branch, tag, or commit a deployment actually names, observed so the
// isolated overlay for that ref can be admitted. It is the fact side of the
// RefSnapshot join the graph gains in a sibling Phase 1 leaf; the fact
// itself carries only the ref identity and its resolution state, never file
// content or dependency rows, so observations stay drift-sized and can never
// merge into canonical graph truth.
//
// RepoID, Ref, RefKind, and Resolvability are required: an observation with
// no repo, no ref name, no object class, or no resolution state is not
// attributable to any overlay. HeadSHA is optional: nil whenever the ref is
// unresolved (ResolvabilityUnresolved) or the fetch failed — an unreachable
// ref reads unknown, never the default branch's SHA. CollectorInstanceID
// stays optional: it identifies the collector run, not the observation.
type BranchObservation struct {
	// CollectorInstanceID identifies the collector instance run that emitted
	// this fact. Optional: it is operational metadata about the run, not part
	// of the observation's identity.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`

	// RepoID is the canonical repo_id of the repository the tracked ref
	// belongs to. Required: without it the fact cannot be attributed to any
	// repository.
	RepoID string `json:"repo_id"`

	// Ref is the tracked ref as the deployment names it: a branch name, a
	// tag name, or a commit SHA. Required: it is the overlay identity
	// (repo_id@ref) half that distinguishes this observation from the
	// default branch.
	Ref string `json:"ref"`

	// RefKind is the git object class Ref names: one of RefKindBranch,
	// RefKindTag, or RefKindCommit. Required: branch and tag refs are
	// observed via the live ref list while a commit SHA needs a targeted
	// fetch, so the kind selects the observation path.
	RefKind string `json:"ref_kind"`

	// Resolvability is whether Ref resolves to a concrete commit: one of
	// ResolvabilityPinned, ResolvabilityMobile, ResolvabilityRange, or
	// ResolvabilityUnresolved. Required: an unresolvable ref must be
	// surfaced as unknown, never silently treated as the default branch.
	Resolvability string `json:"resolvability"`

	// HeadSHA is the commit SHA the tracked ref resolved to at observation
	// time. Optional: nil when Resolvability is unresolved (nothing resolved
	// to record) or when the targeted fetch failed — the observation still
	// records that the deployment names this ref.
	HeadSHA *string `json:"head_sha,omitempty"`
}
