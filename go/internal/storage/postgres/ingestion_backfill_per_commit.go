// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// runPostCommitRelationshipBackfill runs the per-commit new-repository
// relationship backfill in its own short transaction, AFTER the caller's main
// ingestion commit has already released the deferred-maintenance shared
// advisory barrier (issue #4451, § T8). Before this split,
// backfillRelationshipEvidenceForNewRepositories ran inside the same locked
// transaction as the atomic scope/generation/fact commit, so the barrier — and
// therefore any same-repo deferred-maintenance exclusive-lock batch waiting on
// it — was held for the backfill's corpus-anchor read and DiscoverEvidence
// pass too, not just the atomic write.
//
// This method re-acquires the barrier for only its own bounded backfill write,
// so a same-repo deferred-maintenance batch now waits at most for whichever of
// the two short critical sections (the atomic commit or this backfill) it
// overlaps, never both combined. Lock ordering is unchanged (same
// deferredMaintenanceRepoLockKey), so this introduces no new deadlock class:
// see TestIngestionCommitAndDeferredMaintenanceNeverDeadlocks.
//
// Errors are logged, never returned: the generation this call enriches is
// already durably committed by the time this runs, so surfacing an error here
// would make collector.Service.Run treat an already-successful commit as a
// commit_failure and dead-letter/retry an already-landed generation. The
// periodic corpus-wide BackfillAllRelationshipEvidence pass (cmd/ingester)
// already re-discovers evidence for every active repository on its own
// schedule, so a failure here only delays — never drops — the evidence.
func (s IngestionStore) runPostCommitRelationshipBackfill(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	knownRepoIDs map[string]struct{},
	currentGenerationRepos map[string]relationships.CatalogEntry,
) {
	newRepoIDs := catalogEntryIDSet(currentGenerationRepos)
	if len(newRepoIDs) == 0 {
		return
	}
	if s.beginner == nil {
		return
	}

	start := time.Now()
	if err := s.commitPostCommitRelationshipBackfillTx(ctx, scopeValue, generation, knownRepoIDs, newRepoIDs); err != nil {
		if s.Logger != nil {
			s.Logger.ErrorContext(
				ctx, "post-commit relationship backfill failed",
				slog.String("scope_id", scopeValue.ScopeID),
				slog.String("generation_id", generation.GenerationID),
				slog.Float64("duration_seconds", time.Since(start).Seconds()),
				slog.String("error", err.Error()),
			)
		}
		return
	}
	s.logCommitStage(ctx, scopeValue, generation, "post_commit_relationship_backfill", start)
}

// commitPostCommitRelationshipBackfillTx takes the deferred-maintenance shared
// barrier for scopeValue's repository partition and runs the new-repository
// relationship backfill, all inside one short transaction separate from the
// caller's main ingestion commit.
func (s IngestionStore) commitPostCommitRelationshipBackfillTx(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	knownRepoIDs map[string]struct{},
	newRepoIDs map[string]struct{},
) error {
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin post-commit relationship backfill transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := acquireDeferredMaintenanceRepoSharedLock(ctx, tx, deferredMaintenanceRepoLockKey(scopeValue)); err != nil {
		return fmt.Errorf("acquire deferred maintenance shared barrier: %w", err)
	}

	relationshipStore := NewRelationshipStore(tx)
	if err := backfillRelationshipEvidenceForNewRepositories(
		ctx, tx, relationshipStore, generation.GenerationID, knownRepoIDs, newRepoIDs,
	); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit post-commit relationship backfill transaction: %w", err)
	}
	committed = true
	return nil
}

// recordSharedLockHoldDuration reports how long CommitScopeGeneration held the
// deferred-maintenance shared advisory barrier for one atomic commit (issue
// #4451, § T8). It is the operator-visible proof that the lock split shrank
// the hold window to the atomic commit, since the per-commit relationship
// backfill (previously run inside that same window) now runs after release in
// runPostCommitRelationshipBackfill.
func (s IngestionStore) recordSharedLockHoldDuration(ctx context.Context, d time.Duration) {
	if s.Instruments == nil || s.Instruments.IngestionSharedLockHoldDuration == nil {
		return
	}
	s.Instruments.IngestionSharedLockHoldDuration.Record(ctx, d.Seconds())
}

func backfillRelationshipEvidenceForNewRepositories(
	ctx context.Context,
	queryer Queryer,
	relationshipStore *RelationshipStore,
	generationID string,
	knownRepoIDs map[string]struct{},
	currentGenerationRepoIDs map[string]struct{},
) error {
	if relationshipStore == nil || len(currentGenerationRepoIDs) == 0 {
		return nil
	}

	newRepoIDs := make(map[string]struct{})
	for repoID := range currentGenerationRepoIDs {
		if _, exists := knownRepoIDs[repoID]; exists {
			continue
		}
		newRepoIDs[repoID] = struct{}{}
	}
	if len(newRepoIDs) == 0 {
		return nil
	}

	refreshedCatalog, err := loadRepositoryCatalog(ctx, queryer)
	if err != nil {
		return fmt.Errorf("reload repository catalog for relationship backfill: %w", err)
	}

	// Derive payload anchors from only the newly onboarded repositories' catalog
	// aliases (issue #3570) and load just the latest-generation content, file, and
	// gcp_cloud_relationship facts whose payload could reference one of them, plus
	// the always-loaded ArgoCD-shaped facts and their external config files. This
	// replaces an O(all-repos) full-corpus fact load that shipped every
	// repository's facts on every onboarding commit even though the scoped catalog
	// could only match the onboarding delta. The anchor predicate is a provable
	// superset of the facts DiscoverEvidence could match against newRepoCatalog, so
	// no evidence is dropped. If no anchors exist (the new repos have no usable
	// aliases) no fact can match, so the backfill short-circuits.
	newRepoCatalog := repositoryScopedCatalog(refreshedCatalog, newRepoIDs)
	activeFacts, err := loadAnchorScopedRelationshipFacts(ctx, queryer, newRepoCatalog, refreshedCatalog)
	if err != nil {
		return fmt.Errorf("load anchor-scoped facts for relationship backfill: %w", err)
	}
	if len(activeFacts) == 0 {
		return nil
	}

	// Scope the catalog matcher to only the repositories this generation
	// onboarded (issue #3500) plus the source repos of any gcp_cloud_relationship
	// fact targeting them. DiscoverEvidence is a pure function of
	// (envelopes, catalog); for content-derived evidence every emitted
	// EvidenceFact.TargetRepoID is a catalog entry, so a new-repo-scoped catalog
	// reproduces exactly what the prior full-catalog discovery produced and then
	// discarded via filterEvidenceByTargetRepo — minus the O(all-repos) matcher
	// build and the post-filter pass. GCP relationship facts are the exception:
	// discoverGCPCloudRelationshipEvidence resolves the SOURCE resource against
	// the catalog before the target, so an old-source -> new-target edge needs the
	// source repo's catalog entry too. backfillScopedCatalog adds exactly those
	// source entries (scoped to facts whose target is a new repo), never the whole
	// fleet.
	scopedCatalog := backfillScopedCatalog(refreshedCatalog, activeFacts, newRepoIDs)
	if len(scopedCatalog) == 0 {
		return nil
	}
	evidence := relationships.DedupeEvidenceFacts(
		relationships.DiscoverEvidence(activeFacts, scopedCatalog),
	)
	if len(evidence) == 0 {
		return nil
	}
	if err := relationshipStore.UpsertEvidenceFacts(ctx, generationID, evidence); err != nil {
		return fmt.Errorf("persist backfilled relationship evidence: %w", err)
	}

	return nil
}

// backfillScopedCatalog returns the catalog entries the per-commit relationship
// backfill must match against: every newly onboarded repository, plus the source
// repositories of any supported gcp_cloud_relationship fact whose target is one
// of those new repositories (issue #3500), plus the external config repositories
// any loaded ArgoCD ApplicationSet's git generator targets (issue #3570).
//
// Most content-derived evidence (Terraform, Helm, Kustomize, ...) carries its
// source repo in the fact envelope and only catalog-matches the target, so a
// new-repo-scoped catalog suffices. Two relationship classes resolve an
// intermediate repository against the catalog before the target and so need that
// intermediate entry present even though it is not a new repo:
//
//   - GCP relationship facts: discoverGCPCloudRelationshipEvidence first requires
//     a unique catalog match for the SOURCE resource and only then matches the
//     target, so an old-source -> new-target edge resolves to nothing unless the
//     source repo's catalog entry is present. ResolveGCPRelationshipRepoLinks
//     adds only the source entries that feed a new target.
//   - ArgoCD ApplicationSet deploy edges: discoverArgoCDDocumentEvidence first
//     catalog-matches the git generator's config repoURL, then renders the deploy
//     repoURL from that config repo's files; the deploy edge resolves to nothing
//     unless the config repo's catalog entry is present.
//     ResolveArgoCDGeneratorConfigRepos adds those config repo entries. Adding a
//     config repo cannot create a spurious edge: the deploy target must still be a
//     catalog entry (a new repo), and the config repo itself is excluded as a
//     deploy target by discovery.
//
// The scope therefore stays bounded to the onboarding delta plus its inbound GCP
// sources and ArgoCD config repos, never the whole fleet, preserving the
// scope-bounding performance win.
func backfillScopedCatalog(
	catalog []relationships.CatalogEntry,
	activeFacts []facts.Envelope,
	newRepoIDs map[string]struct{},
) []relationships.CatalogEntry {
	if len(catalog) == 0 || len(newRepoIDs) == 0 {
		return nil
	}

	scopeRepoIDs := make(map[string]struct{}, len(newRepoIDs))
	for repoID := range newRepoIDs {
		scopeRepoIDs[repoID] = struct{}{}
	}
	for _, link := range relationships.ResolveGCPRelationshipRepoLinks(activeFacts, catalog) {
		if _, targetIsNew := newRepoIDs[link.TargetRepoID]; !targetIsNew {
			continue
		}
		scopeRepoIDs[link.SourceRepoID] = struct{}{}
	}
	for _, ref := range relationships.ResolveArgoCDGeneratorConfigRepos(activeFacts, catalog) {
		scopeRepoIDs[ref.ConfigRepoID] = struct{}{}
	}

	return repositoryScopedCatalog(catalog, scopeRepoIDs)
}

// repositoryScopedCatalog returns the subset of catalog entries whose RepoID is
// in repoIDs, preserving each entry's aliases verbatim. It is the scope-bounded
// matcher input for the per-commit relationship backfill (issue #3500): the
// matcher and the per-fact match cost scale with the onboarding delta, not the
// fleet size, while correlation truth is unchanged because DiscoverEvidence
// already keys every emitted EvidenceFact.TargetRepoID to a catalog entry.
func repositoryScopedCatalog(
	catalog []relationships.CatalogEntry,
	repoIDs map[string]struct{},
) []relationships.CatalogEntry {
	if len(catalog) == 0 || len(repoIDs) == 0 {
		return nil
	}

	scoped := make([]relationships.CatalogEntry, 0, len(repoIDs))
	for _, entry := range catalog {
		if _, ok := repoIDs[entry.RepoID]; !ok {
			continue
		}
		scoped = append(scoped, entry)
	}
	return scoped
}

func filterEvidenceByTargetRepo(
	evidence []relationships.EvidenceFact,
	targetRepoIDs map[string]struct{},
) []relationships.EvidenceFact {
	if len(evidence) == 0 || len(targetRepoIDs) == 0 {
		return nil
	}

	filtered := make([]relationships.EvidenceFact, 0, len(evidence))
	for _, fact := range evidence {
		if _, ok := targetRepoIDs[fact.TargetRepoID]; !ok {
			continue
		}
		filtered = append(filtered, fact)
	}
	return filtered
}
