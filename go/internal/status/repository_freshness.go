// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

import (
	"strings"
	"time"
)

// RepositoryFreshnessVerdict is the coarse verdict rendered for
// GET /api/v0/repositories/{id}/freshness (issue #5143): does eshu's
// evidence for one repository reflect its latest commit, and is that
// evidence fully built.
type RepositoryFreshnessVerdict string

// The five verdicts a repository freshness read can render. See
// ComputeRepositoryFreshnessVerdict for the precedence between them.
const (
	RepositoryFreshnessCurrent    RepositoryFreshnessVerdict = "current"
	RepositoryFreshnessBuilding   RepositoryFreshnessVerdict = "building"
	RepositoryFreshnessBehind     RepositoryFreshnessVerdict = "behind"
	RepositoryFreshnessUnobserved RepositoryFreshnessVerdict = "unobserved"
	RepositoryFreshnessUnknown    RepositoryFreshnessVerdict = "unknown"
)

// RepositoryFreshnessGeneration is the resolved generation lifecycle snapshot
// backing a freshness read, mirroring the fields generationTransitionsQuery
// already exposes on the status surface.
type RepositoryFreshnessGeneration struct {
	ID          string
	Status      string
	TriggerKind string
	IsDelta     bool
	// ActivatedAt is the zero time when the generation has never activated
	// (for example a pending first sync).
	ActivatedAt time.Time
}

// RepositoryFreshnessStages reports whether each pipeline phase has drained
// for the resolved (scope, generation): no outstanding fact_work_items row in
// a non-terminal-success status. Collected is true whenever a generation was
// resolved at all -- collection precedes the scope_generations row's own
// existence, so resolving one already proves the source snapshot landed.
// Materialized mirrors SharedEnrichment.Pending in its drained (not pending)
// form so the stage checklist and the cross-repo backlog detail agree by
// construction.
type RepositoryFreshnessStages struct {
	Collected    bool
	Reduced      bool
	Projected    bool
	Materialized bool
}

// RepositoryFreshnessOutstanding is one (stage, status) count row for the
// resolved generation, mirroring stageCountsQuery's shape
// (storage/postgres/status_queries.go) scoped to a single scope/generation.
type RepositoryFreshnessOutstanding struct {
	Stage  string
	Status string
	Count  int
}

// RepositoryFreshnessPendingDomain is one shared cross-repo projection
// domain with outstanding (completed_at IS NULL) intents for the resolved
// generation.
type RepositoryFreshnessPendingDomain struct {
	Domain string
	Count  int
}

// RepositoryFreshnessSharedEnrichment reports cross-repo shared-projection
// backlog for the resolved generation. This is a separate axis from Stages:
// a repo's own reducer/projector work can be fully drained while shared
// enrichment referencing its generation is still outstanding, and that
// backlog must never be attributed to a different repository's freshness.
type RepositoryFreshnessSharedEnrichment struct {
	Pending        bool
	PendingDomains []RepositoryFreshnessPendingDomain
}

// RepositoryFreshnessUnobservedPush is a queued or claimed webhook refresh
// trigger for this repository whose target commit does not match the
// resolved generation's observed commit -- eshu has not started building
// this push yet.
type RepositoryFreshnessUnobservedPush struct {
	TargetSHA  string
	Ref        string
	ReceivedAt time.Time
}

// RepositoryFreshnessSnapshot is the raw evidence read for one repository,
// before verdict computation. It never fabricates a value: Resolved and
// HasGeneration report exactly what was found so the verdict function and the
// HTTP handler can represent an unresolved or ungenerated repository
// honestly rather than defaulting to a false "current".
type RepositoryFreshnessSnapshot struct {
	RepositoryID string
	ScopeID      string
	// Resolved reports whether repo_id resolved to an ingestion scope at
	// all (via the current active generation's repository fact).
	Resolved bool
	// ScopeKind is the resolved scope's kind (for example "repository").
	// Empty when Resolved is false.
	ScopeKind string
	// HasGeneration reports whether the resolved scope carries a
	// resolvable generation. False for a resolved scope with no active or
	// latest generation, which should not happen in practice but is
	// represented honestly rather than assumed.
	HasGeneration bool
	Generation    RepositoryFreshnessGeneration
	// ObservedCommit is the resolved generation's source commit SHA. Empty
	// is legitimate for non-git scopes, for pre-delta-baseline git
	// generations that predate the source_commit_sha column, and for
	// snapshot-trigger git generations (trigger_kind="snapshot": a
	// cassette-replayed or otherwise non-live-git-sync source with no
	// commit to report, as opposed to a push/delta-triggered sync). A
	// snapshot-trigger generation can still be fully built -- verdict
	// "current" there means build completeness, not a commit receipt;
	// represent the empty SHA explicitly rather than fabricating a value.
	ObservedCommit   string
	ObservedAt       time.Time
	Stages           RepositoryFreshnessStages
	Outstanding      []RepositoryFreshnessOutstanding
	SharedEnrichment RepositoryFreshnessSharedEnrichment
	// UnobservedPush is nil when no queued/claimed webhook push evidence
	// exists for this repository.
	UnobservedPush *RepositoryFreshnessUnobservedPush
}

// ComputeRepositoryFreshnessVerdict derives the coarse verdict from a
// resolved snapshot and an optional caller-supplied expected commit SHA
// (empty when absent). It is a pure function so every verdict branch --
// including the empty-observed-commit and shared-pending-only cases -- is
// unit testable without a database.
//
// Precedence (evaluated top to bottom; the first matching rule wins):
//
//  1. unknown: no scope/generation resolved for this repository, or the
//     resolved scope is not a git ("repository") scope and carries no
//     commit -- freshness-by-commit is not a meaningful question for it.
//  2. unobserved: a queued/claimed webhook push exists whose target commit
//     does not match the observed commit -- eshu has not even started
//     building it.
//  3. behind: the caller supplied expected_commit and it does not match
//     observed_commit. This takes precedence over building/current:
//     whether or not a generation is actively catching up, the answer does
//     not yet reflect the caller's expected commit, and that is the
//     accurate, actionable state for "did eshu pick up my commit".
//  4. building: the repository's own reducer/projector stage has
//     outstanding work, or shared cross-repo enrichment referencing this
//     generation is still pending.
//  5. current: own stages drained, no shared pending, and (no
//     expected_commit was supplied, or it matches observed_commit). This
//     speaks to BUILD COMPLETENESS, not necessarily a commit receipt:
//     observed_commit may be empty (non-git scopes, pre-delta-baseline
//     generations, or snapshot-trigger git generations) while the verdict is
//     still honestly "current" for that generation's own evidence.
func ComputeRepositoryFreshnessVerdict(snapshot RepositoryFreshnessSnapshot, expectedCommit string) RepositoryFreshnessVerdict {
	expectedCommit = strings.TrimSpace(expectedCommit)

	if !snapshot.Resolved || !snapshot.HasGeneration {
		return RepositoryFreshnessUnknown
	}
	if snapshot.ObservedCommit == "" && snapshot.ScopeKind != "" && snapshot.ScopeKind != "repository" {
		return RepositoryFreshnessUnknown
	}
	if snapshot.UnobservedPush != nil {
		return RepositoryFreshnessUnobserved
	}
	if expectedCommit != "" && expectedCommit != snapshot.ObservedCommit {
		return RepositoryFreshnessBehind
	}
	if !snapshot.Stages.Reduced || !snapshot.Stages.Projected || snapshot.SharedEnrichment.Pending {
		return RepositoryFreshnessBuilding
	}
	return RepositoryFreshnessCurrent
}
