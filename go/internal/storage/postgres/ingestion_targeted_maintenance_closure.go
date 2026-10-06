// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// targetedMaintenanceMaxClosureRounds bounds how many times the closure may
// promote a dependent partition that never committed backward evidence into
// the owed set. Each round adds at least one partition, so the bound caps the
// work one obligation can pull in; exceeding it is an error, never a silent
// truncation of the affected set.
const targetedMaintenanceMaxClosureRounds = 8

// targetedMaintenanceClosure is the input set of one partition-scoped pass.
type targetedMaintenanceClosure struct {
	// owed are the requested partitions whose generation is still the scope's
	// active one, plus every promoted partition.
	owed map[scopeGenerationPartition]struct{}
	// promoted are dependent partitions that had no committed
	// backward_evidence phase and were therefore processed as owed.
	promoted []scopeGenerationPartition
	// load are the (scope_id, generation_id) partitions whose facts the pass
	// loads through the memo-gated deferred loader: the owed partitions and
	// every partition holding a fact whose evidence touches an owed repository.
	load map[scopeGenerationPartition]struct{}
	// affectedRepos are the repositories whose evidence the pass writes and
	// whose partitions it publishes and reopens: the owed repositories and the
	// source repositories of evidence that touches them.
	affectedRepos map[string]repositoryGenerationIdentity
}

// affectedPartitions returns the distinct active partitions of affectedRepos.
func (c targetedMaintenanceClosure) affectedPartitions() map[scopeGenerationPartition]struct{} {
	partitions := make(map[scopeGenerationPartition]struct{}, len(c.affectedRepos))
	for _, identity := range c.affectedRepos {
		partitions[scopeGenerationPartition{ScopeID: identity.ScopeID, GenerationID: identity.GenerationID}] = struct{}{}
	}
	return partitions
}

// resolveTargetedMaintenanceClosure computes which partitions a pass for the
// owed partitions must load, write, publish and reopen so its result for them
// equals the whole pass's result for them.
//
// Relationship resolution reads only the evidence attached to its own
// generation (crossrepo ListEvidenceFacts by generation), and evidence is
// attached to the SOURCE repository's active generation. So the partitions a
// change to an owed repository R affects are R's own partition and the active
// partitions of the source repositories of evidence whose source or target is
// R. Those sources may live anywhere: another git scope that references R in
// its content, a cloud scope with no repository fact whose GCP relationship
// resolves to some source repository, or an ArgoCD ApplicationSet whose git
// generator reads an owed repository as its external config repo. The
// target-catalog inbound loader (loadAnchorScopedRelationshipFacts) is a proven
// superset of the facts that can match R (the #7584 read-side parity proof), so
// grouping its facts by partition and discovering each group's evidence finds
// every such source.
//
// A dependent partition with no committed backward_evidence phase has never
// been processed; publishing its phase without loading its own facts would
// claim evidence that was never derived. Such a partition is promoted into the
// owed set and the closure is recomputed, at most
// targetedMaintenanceMaxClosureRounds times.
func (s IngestionStore) resolveTargetedMaintenanceClosure(
	ctx context.Context,
	catalog []relationships.CatalogEntry,
	params deferredScopedFactQueryParams,
	hasAnchors bool,
	active map[scopeGenerationPartition]struct{},
) (targetedMaintenanceClosure, error) {
	closure := targetedMaintenanceClosure{owed: make(map[scopeGenerationPartition]struct{}, len(active))}
	for partition := range active {
		closure.owed[partition] = struct{}{}
	}

	for round := 0; ; round++ {
		if round >= targetedMaintenanceMaxClosureRounds {
			return closure, fmt.Errorf("%w: %d rounds, owed=%d",
				ErrTargetedMaintenanceClosureTooDeep, targetedMaintenanceMaxClosureRounds, len(closure.owed))
		}
		owedRepos, err := loadActiveRepositoryGenerationsForPartitions(ctx, s.database, sortedPartitions(closure.owed))
		if err != nil {
			return closure, fmt.Errorf("load owed repository generations: %w", err)
		}
		load := make(map[scopeGenerationPartition]struct{}, len(closure.owed))
		sourceRepoIDs := make(map[string]struct{}, len(owedRepos))
		for partition := range closure.owed {
			load[partition] = struct{}{}
		}
		for repoID := range owedRepos {
			sourceRepoIDs[repoID] = struct{}{}
		}

		// Evidence derived from the owed partitions' own facts: every source of
		// it is affected, including a source repository that a cloud-scope fact
		// in an owed partition resolves to.
		if hasAnchors && len(closure.owed) > 0 {
			ownFacts, _, err := s.loadDeferredScopedFactsAcrossPartitions(
				ctx, s.database, params, sortedPartitions(closure.owed), nil)
			if err != nil {
				return closure, fmt.Errorf("load owed partition facts for closure: %w", err)
			}
			if len(ownFacts) > 0 {
				ownFacts, err = s.appendArgoCDGeneratorConfigFacts(ctx, s.database, catalog, ownFacts)
				if err != nil {
					return closure, err
				}
			}
			for _, evidence := range relationships.DiscoverEvidence(ownFacts, catalog) {
				if evidence.SourceRepoID != "" && evidence.TargetRepoID != "" {
					sourceRepoIDs[evidence.SourceRepoID] = struct{}{}
				}
			}
		}

		// Evidence that touches an owed repository from any other partition.
		if hasAnchors && len(owedRepos) > 0 {
			if err := s.addInboundTargetedMaintenanceSources(
				ctx, catalog, owedRepos, load, sourceRepoIDs,
			); err != nil {
				return closure, err
			}
		}

		ids := make([]string, 0, len(sourceRepoIDs))
		for repoID := range sourceRepoIDs {
			ids = append(ids, repoID)
		}
		sort.Strings(ids)
		affected, err := loadActiveRepositoryGenerationsForRepos(ctx, s.database, ids)
		if err != nil {
			return closure, fmt.Errorf("load affected repository generations: %w", err)
		}

		promotedThisRound := 0
		for _, identity := range affected {
			partition := scopeGenerationPartition{ScopeID: identity.ScopeID, GenerationID: identity.GenerationID}
			if _, owed := closure.owed[partition]; owed {
				continue
			}
			committed, err := backwardEvidencePhaseCommitted(ctx, s.database, partition)
			if err != nil {
				return closure, err
			}
			if committed {
				continue
			}
			closure.owed[partition] = struct{}{}
			closure.promoted = append(closure.promoted, partition)
			promotedThisRound++
		}
		if promotedThisRound > 0 {
			continue
		}
		closure.load = load
		closure.affectedRepos = affected
		promoted := make(map[scopeGenerationPartition]struct{}, len(closure.promoted))
		for _, partition := range closure.promoted {
			promoted[partition] = struct{}{}
		}
		closure.promoted = sortedPartitions(promoted)
		return closure, nil
	}
}

// addInboundTargetedMaintenanceSources loads the facts that can match an owed
// repository, groups them by partition, and records every group whose own
// evidence touches an owed repository in load, and the sources of that
// evidence in sourceRepoIDs.
//
// Each group's evidence is discovered from the group plus the external ArgoCD
// config files its own ApplicationSets reference, because the ApplicationSet
// renders its deploy edges from those files. Evidence the config files would
// produce on their own is attributed to the group too; that can only widen the
// affected set, and a wider set still equals the whole pass on every partition
// it holds (the whole pass touches all of them).
//
// An owed external config repository needs no rule of its own: every
// ApplicationSet evidence kind targets the config repository (relationships
// appendDiscoveryEvidence and appendDeploySourceEvidence), so the control
// repository's group touches it.
func (s IngestionStore) addInboundTargetedMaintenanceSources(
	ctx context.Context,
	catalog []relationships.CatalogEntry,
	owedRepos map[string]repositoryGenerationIdentity,
	load map[scopeGenerationPartition]struct{},
	sourceRepoIDs map[string]struct{},
) error {
	owedRepoIDs := make(map[string]struct{}, len(owedRepos))
	for repoID := range owedRepos {
		owedRepoIDs[repoID] = struct{}{}
	}
	inbound, err := loadAnchorScopedRelationshipFacts(
		ctx, s.database, repositoryScopedCatalog(catalog, owedRepoIDs), catalog)
	if err != nil {
		return fmt.Errorf("load inbound facts for owed repositories: %w", err)
	}

	groups := make(map[scopeGenerationPartition][]facts.Envelope)
	for _, envelope := range inbound {
		partition := scopeGenerationPartition{ScopeID: envelope.ScopeID, GenerationID: envelope.GenerationID}
		groups[partition] = append(groups[partition], envelope)
	}
	for _, partition := range sortedPartitionsOf(groups) {
		group := groups[partition]
		var configFacts []facts.Envelope
		if refs := relationships.ResolveArgoCDGeneratorConfigRepos(group, catalog); len(refs) > 0 {
			configRepoIDs := make([]string, 0, len(refs))
			for _, ref := range refs {
				configRepoIDs = append(configRepoIDs, ref.ConfigRepoID)
			}
			configFacts, err = loadArgoCDGeneratorConfigFacts(ctx, s.database, configRepoIDs)
			if err != nil {
				return fmt.Errorf("load argocd config facts for closure group %q: %w", partition.ScopeID, err)
			}
		}
		groupEvidence := relationships.DedupeEvidenceFacts(
			relationships.DiscoverEvidence(mergeRelationshipFacts(group, configFacts), catalog))
		touched := false
		for _, evidence := range groupEvidence {
			if evidence.SourceRepoID == "" || evidence.TargetRepoID == "" {
				continue
			}
			_, sourceOwed := owedRepoIDs[evidence.SourceRepoID]
			_, targetOwed := owedRepoIDs[evidence.TargetRepoID]
			if !sourceOwed && !targetOwed {
				continue
			}
			touched = true
			sourceRepoIDs[evidence.SourceRepoID] = struct{}{}
		}
		if touched {
			load[partition] = struct{}{}
		}
	}
	return nil
}

// backwardEvidencePhaseCommitted reports whether partition already carries the
// backward_evidence_committed readiness row publishDeferredBackfillPartition
// writes.
func backwardEvidencePhaseCommitted(
	ctx context.Context,
	database db.ExecQueryer,
	partition scopeGenerationPartition,
) (bool, error) {
	ready, found, err := NewGraphProjectionPhaseStateStore(database).Lookup(ctx, reducer.GraphProjectionPhaseKey{
		ScopeID:          partition.ScopeID,
		AcceptanceUnitID: partition.ScopeID,
		SourceRunID:      partition.GenerationID,
		GenerationID:     partition.GenerationID,
		Keyspace:         reducer.GraphProjectionKeyspaceCrossRepoEvidence,
	}, reducer.GraphProjectionPhaseBackwardEvidenceCommitted)
	if err != nil {
		return false, fmt.Errorf("read backward evidence phase for %q/%q: %w", partition.ScopeID, partition.GenerationID, err)
	}
	return found && ready, nil
}

// sortedPartitionsOf returns the keys of groups in (scope_id, generation_id)
// order.
func sortedPartitionsOf(groups map[scopeGenerationPartition][]facts.Envelope) []scopeGenerationPartition {
	set := make(map[scopeGenerationPartition]struct{}, len(groups))
	for partition := range groups {
		set[partition] = struct{}{}
	}
	return sortedPartitions(set)
}
