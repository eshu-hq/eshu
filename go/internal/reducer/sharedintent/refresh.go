// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sharedintent

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

const (
	// RepoRefreshIntentType is the payload intent_type marking a per-repo
	// refresh intent -- the one row that owns a repo-wide-retract domain's
	// single retract (#2898).
	//
	// It is one constant rather than a literal per emitter because the
	// graph-write side reads it back: storage/cypher's rationale retract
	// collects whole-scope repository ids by matching this intent_type, and if
	// the two sides drifted that predicate would match nothing, the whole-scope
	// retract would silently stop running, and stale edges would persist with
	// no error and no dead letter (#5998).
	RepoRefreshIntentType = "repo_refresh"

	// RepoRefreshAction is the payload action a per-repo refresh intent carries.
	RepoRefreshAction = "refresh"

	// RetractViaRefreshKey marks a per-edge row that was emitted WITH a paired
	// repo refresh intent, so the worker may safely fence it behind that
	// refresh. Per-edge rows without the marker predate the #2898 emission (no
	// paired refresh exists for their source run), so the worker keeps them on
	// the legacy per-partition retract path rather than deferring them forever.
	// Such in-flight rows drain normally and are superseded by the next
	// re-ingest's marked rows.
	RetractViaRefreshKey = "retract_via_refresh"
)

// RepoWideRetractRefreshPartitionKey is the whole-scope partition key the
// per-repo refresh intent is emitted under and that the worker reconstructs to
// fence a per-edge row. A whole-scope key hashes to exactly one partition, so a
// repo's single repo-wide retract is owned by one partition lease and cannot
// race itself. Emission and the fence MUST build the key identically, so they
// share this helper.
func RepoWideRetractRefreshPartitionKey(domain, repoID string) string {
	return domain + ":refresh:v1:whole:" + strings.TrimSpace(repoID)
}

// DeltaScopeRepositorySet indexes the repositories a delta scope reports as
// being on a delta generation. The fenced repo-wide-retract domains
// (inheritance, rationale, SQL relationships, shell exec) build it once per
// refresh-intent batch and hand it to [ApplyRepoRefreshDeltaScope] per
// repository, so the gate stays O(1) per repository.
func DeltaScopeRepositorySet(repositoryIDs []string) map[string]struct{} {
	if len(repositoryIDs) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		set[repositoryID] = struct{}{}
	}
	return set
}

// ApplyRepoRefreshDeltaScope stamps the delta retract scope onto one
// repository's repo-wide refresh payload.
//
// The gate is membership in deltaRepositoryIDs -- the repositories whose
// repository fact carried delta_generation this cycle. It is deliberately NOT
// the scope-wide hasDelta flag, and deliberately NOT "this repository has
// qualified paths". All three readings differ, and two of them are wrong
// (#6216):
//
//   - On a delta generation, ALWAYS delta-scoped, even with an empty path list.
//     The collector replaces the discovered file set with the changed targets
//     alone on a delta sync (resolveNativeSnapshotFileSetForTargets,
//     collector/gitrepo/git_snapshot_native.go), so the generation carries
//     content-entity facts for the CHANGED files only and the per-edge intents
//     re-create only those files' edges. Widening such a repository's retract to
//     the whole repository deletes every UNCHANGED file's edge with nothing left
//     to restore it: silent wrong graph, no error, no dead letter. An empty list
//     instead reaches collectDeltaFilePaths
//     (storage/cypher/edge_writer_retract_scope.go), which rejects it before any
//     statement runs, so the partition fails and the intent dead-letters. That
//     is the intended outcome -- a dead letter an operator can see beats a graph
//     that quietly lost edges. A repository reaches this state when its changed
//     paths cannot be qualified: no local_path on the repository fact, or a
//     symlinked repos root whose targets normalizeSnapshotRelativePaths drops.
//
//   - On a FULL generation, never delta-scoped, even when a delta-generation
//     repository shares the scope. hasDelta is scope-wide, so gating on it would
//     scope a full-generation repository's retract to a sibling's changed paths
//     and leave its removed-file edges behind. Its own generation re-emits every
//     file, so the repo-wide retract is the correct scope for it.
func ApplyRepoRefreshDeltaScope(
	payload map[string]any,
	repoID string,
	deltaRepositoryIDs map[string]struct{},
	filePathsByRepoID map[string][]string,
) {
	if _, onDeltaGeneration := deltaRepositoryIDs[repoID]; !onDeltaGeneration {
		return
	}
	filePaths := filePathsByRepoID[repoID]
	payload["delta_projection"] = true
	payload["delta_file_paths"] = append(make([]string, 0, len(filePaths)), filePaths...)
}

// IsRepoRefreshRow reports whether a row is a per-repo refresh intent.
func IsRepoRefreshRow(row Row) bool {
	return payloadcore.PayloadStr(row.Payload, "intent_type") == RepoRefreshIntentType
}

// MarkRowsRetractViaRefresh stamps the retract_via_refresh marker on every
// per-edge row so the worker fences them behind their paired repo refresh
// intent. It is applied at emission, right where the refresh intents are
// built, so the marker and the refresh intent are always emitted together.
func MarkRowsRetractViaRefresh(rows []Row) []Row {
	for i := range rows {
		if rows[i].Payload == nil {
			rows[i].Payload = map[string]any{}
		}
		rows[i].Payload[RetractViaRefreshKey] = true
	}
	return rows
}

// SplitRepoRefreshRows separates per-repo refresh rows from per-edge rows,
// preserving order. A refresh row carries no edge target, so callers exempt
// it from the endpoint-presence (terminal) gate that would otherwise drain
// it with no edge and never run its repo-wide retract.
func SplitRepoRefreshRows(rows []Row) (refresh, edge []Row) {
	for _, row := range rows {
		if IsRepoRefreshRow(row) {
			refresh = append(refresh, row)
			continue
		}
		edge = append(edge, row)
	}
	return refresh, edge
}

// repoWideRetractDomains lists the domains whose retract the per-repo refresh
// intent owns (#2898/#2910); see [DomainHasRepoWideRetract].
//
// The retract the refresh owns may be repo-wide (delete every edge for the
// repo) or file-scoped (delete only the changed files' edges on a delta
// generation): inheritance_edges, sql_relationships, and rationale_edges
// retract repo-wide by default and file-scoped under a delta, while the
// three symbol→runtime domains always retract repo-wide. The fence mechanism
// is identical either way — the refresh intent owns the single retract and
// the per-edge writes are deferred until it commits — because the refresh
// carries whichever delta scope the materializer attached. Repo-keyed
// domains (platform_infra, workload_dependency, …) keep one partition per
// repo, so they do not spread and are intentionally excluded.
//
// The set is written down once, here, because a second copy of it lives in
// internal/storage/cypher's wholeScopeRetractDomains table, which splits
// these same domains into the narrowed and un-narrowed halves of the
// whole-scope retract. A domain added to the fence but missed there gets a
// whole-repository DELETE bound to the batch-wide repository list, which is
// the #6166 over-delete, and nothing in that package's tests would iterate
// over it. [DomainHasRepoWideRetract] and [RepoWideRetractDomains] read this
// one map so TestWholeScopeRetractDomainsCoversFencedSet
// (internal/storage/cypher) compares the two sets rather than two hand-typed
// lists that happen to agree today.
var repoWideRetractDomains = map[string]struct{}{
	contract.DomainHandlesRoute:       {},
	contract.DomainRunsIn:             {},
	contract.DomainInvokesCloudAction: {},
	contract.DomainInheritanceEdges:   {},
	contract.DomainSQLRelationships:   {},
	contract.DomainShellExec:          {},
	contract.DomainRationaleEdges:     {},
}

// DomainHasRepoWideRetract reports whether a domain owns its retract at the
// repository (or whole-repo delta) level rather than per partition. These
// domains emit per-edge partition keys, so their edges spread across
// partitions; the generic worker would otherwise issue the same scope-wide
// retract once per partition and wipe sibling partitions' just-written edges
// within a cycle (#2910). The retract suppression (#2898) routes the single
// retract through a per-repo refresh intent and fences per-edge writes
// behind it.
func DomainHasRepoWideRetract(domain string) bool {
	_, fenced := repoWideRetractDomains[domain]
	return fenced
}

// RepoWideRetractDomains returns every domain whose retract the per-repo
// refresh intent owns, sorted, so another package can check its own handling
// of that set against this one instead of re-enumerating it. It reads the
// same map [DomainHasRepoWideRetract] does, so the two cannot disagree.
func RepoWideRetractDomains() []string {
	domains := make([]string, 0, len(repoWideRetractDomains))
	for domain := range repoWideRetractDomains {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains
}
