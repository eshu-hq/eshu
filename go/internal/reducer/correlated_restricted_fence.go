// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"slices"
	"strings"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/accepted"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// Per-item read-set waits (#10877).
//
// The narrowed path replaces the corpus-wide fence with a per-item wait keyed
// on the producing scopes of exactly the repositories the item reads. The
// store computes the blocking scope set inside the same statement snapshot
// as the restricted verdict and the rows it admits, so a holder that
// activates mid-read cannot slip between a separate verdict and the rows it
// admits (#6740 invariant preserved). The loader needs no new fields: the
// helper below prefers the restricted read when the wired loader offers it
// and falls back to the global path otherwise, so existing wiring (and the
// global accuracy baselines) keeps its behavior byte-identical.
//
// Late-arrival restart is load-bearing, not optional: a scope that resolves
// after the restricted read ran changes the item's answer with nothing
// waking it. Workload materialization and service catalog correlation record
// the same ResolvedReadFingerprint the deployable-unit path records (#8283),
// and the deferred maintenance plus the completion wake reopen items whose
// recorded input no longer matches.

// RestrictedCorpusFencedResolvedRelationshipLoader returns the active
// resolved relationships touching the repository set together with the
// restricted corpus verdict and the verdict-time blocking scope set, all from
// one statement snapshot. complete is false while any blocking scope's
// current relationship generation is retired-or-pending. blockingScopeIDs is
// the exact set the verdict ran under: the deferral parks on it, and the
// activation wake reopens waits naming the activated scope.
type RestrictedCorpusFencedResolvedRelationshipLoader interface {
	GetResolvedRelationshipsForReposWithRestrictedFence(
		ctx context.Context,
		repoIDs []string,
	) ([]relationships.ResolvedRelationship, []string, bool, error)
}

// FingerprintedRestrictedRead is the narrowed read's fingerprint: the
// repository set the read asked for, the blocking scope set the verdict ran
// under, and the input fingerprint of exactly the rows returned.
type FingerprintedRestrictedRead struct {
	RepoIDs          []string
	BlockingScopeIDs []string
	Value            string
}

// FingerprintedRestrictedCorpusFencedResolvedRelationshipLoader is the
// restricted read plus the input fingerprint of the rows it returned,
// computed in the same statement (#10877 parity with #8283). Handlers record
// the fingerprint on success so the reopen gate can restart the item when a
// late-arriving scope changes its input.
type FingerprintedRestrictedCorpusFencedResolvedRelationshipLoader interface {
	GetFingerprintedResolvedRelationshipsForReposWithRestrictedFence(
		ctx context.Context,
		repoIDs []string,
	) ([]relationships.ResolvedRelationship, []string, bool, FingerprintedRestrictedRead, error)
}

// CorpusWaitNarrowing is the Fail-path contract for a corpus-fence deferral
// that parks on a per-item wait instead of the global fence. The queue's
// Fail extracts it from the deferral cause and keys the parked wait on the
// exact verdict-time sets, so the activation wake can reopen the item when
// one of its blockers commits.
type CorpusWaitNarrowing interface {
	// CorpusWaitRepoIDs is the normalized repository set the narrowed read
	// asked for.
	CorpusWaitRepoIDs() []string
	// CorpusWaitBlockingScopeIDs is the verdict-time blocking scope set.
	CorpusWaitBlockingScopeIDs() []string
}

// normalizeCorpusWaitStrings trims, drops empties and duplicates, and sorts,
// so a wait key rendered from two equal sets compares equal.
func normalizeCorpusWaitStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	slices.Sort(out)
	return out
}

// readRestrictedCorpusFencedRelationships loads the resolved relationships a
// workload or deployable-unit pass consumes, gated on the restricted corpus
// fence. With a restricted loader the verdict, the rows, and the blocking
// scope set come from the read's own statement snapshot, and the returned
// read carries the exact verdict-time sets for the deferral's wait key. A
// loader without the restricted read keeps the global path unchanged. The
// own-generation check stays the caller's job and runs first.
func readRestrictedCorpusFencedRelationships(
	ctx context.Context,
	loader ResolvedRelationshipLoader,
	completeLookup accepted.RelationshipGenerationsCompleteLookup,
	intent reducercontract.Intent,
	candidates []WorkloadCandidate,
) (corpusFencedResolvedRead, error) {
	repoIDs := workloadCandidateRepoIDs(candidates)
	if fingerprinted, ok := loader.(FingerprintedRestrictedCorpusFencedResolvedRelationshipLoader); ok && len(candidates) > 0 {
		repoResolved, blocking, complete, fingerprint, err := fingerprinted.
			GetFingerprintedResolvedRelationshipsForReposWithRestrictedFence(ctx, repoIDs)
		if err != nil {
			return corpusFencedResolvedRead{}, err
		}
		read, err := finishFusedResolvedRead(ctx, loader, intent, repoResolved, complete)
		if err != nil {
			return corpusFencedResolvedRead{}, err
		}
		read.narrowed = true
		read.repoIDs = repoIDs
		read.blockingScopeIDs = normalizeCorpusWaitStrings(blocking)
		if read.complete {
			read.fingerprint = reducercontract.ResolvedReadFingerprint{RepoIDs: repoIDs, Value: fingerprint.Value}
			read.fingerprinted = true
		}
		return read, nil
	}
	if restricted, ok := loader.(RestrictedCorpusFencedResolvedRelationshipLoader); ok && len(candidates) > 0 {
		repoResolved, blocking, complete, err := restricted.
			GetResolvedRelationshipsForReposWithRestrictedFence(ctx, repoIDs)
		if err != nil {
			return corpusFencedResolvedRead{}, err
		}
		read, err := finishFusedResolvedRead(ctx, loader, intent, repoResolved, complete)
		if err != nil {
			return corpusFencedResolvedRead{}, err
		}
		read.narrowed = true
		read.repoIDs = repoIDs
		read.blockingScopeIDs = normalizeCorpusWaitStrings(blocking)
		return read, nil
	}
	return readCorpusFencedResolvedRelationships(ctx, loader, completeLookup, intent, candidates)
}

// readDeployableUnitRestrictedInput is readDeployableUnitResolvedInput on the
// narrowed path: the fingerprinted restricted read when the loader offers
// it, else the deployable-unit global path unchanged (including the
// empty-candidate fingerprinted-as-empty rule). A run with no candidate reads
// no by-repos input, so its output cannot change with resolution.
func readDeployableUnitRestrictedInput(
	ctx context.Context,
	loader ResolvedRelationshipLoader,
	completeLookup accepted.RelationshipGenerationsCompleteLookup,
	intent reducercontract.Intent,
	candidates []WorkloadCandidate,
) (corpusFencedResolvedRead, error) {
	if _, ok := loader.(FingerprintedRestrictedCorpusFencedResolvedRelationshipLoader); ok && len(candidates) > 0 {
		return readRestrictedCorpusFencedRelationships(ctx, loader, completeLookup, intent, candidates)
	}
	if _, ok := loader.(RestrictedCorpusFencedResolvedRelationshipLoader); ok && len(candidates) > 0 {
		return readRestrictedCorpusFencedRelationships(ctx, loader, completeLookup, intent, candidates)
	}
	return readDeployableUnitResolvedInput(ctx, loader, completeLookup, intent, candidates)
}

// isRestrictedCorpusFencedLoader reports whether the loader offers the
// narrowed restricted read, so callers that special-case the fused global
// loader (the #9697 pre-gate) treat the restricted loader the same way.
func isRestrictedCorpusFencedLoader(loader ResolvedRelationshipLoader) bool {
	if _, ok := loader.(RestrictedCorpusFencedResolvedRelationshipLoader); ok {
		return true
	}
	_, ok := loader.(FingerprintedRestrictedCorpusFencedResolvedRelationshipLoader)
	return ok
}

// corpusWaitNarrowing carries the verdict-time sets onto the deferral errors
// the queue's Fail path parks on. Nil-safe: a global-path read carries no
// sets and parks on the coarse wait as before.
func corpusWaitNarrowing(read corpusFencedResolvedRead) (repoIDs, blockingScopeIDs []string, ok bool) {
	if !read.narrowed {
		return nil, nil, false
	}
	return read.repoIDs, read.blockingScopeIDs, true
}

// CorpusWaitRepoIDs reports the narrowed repository set of a workload
// materialization deferral for the queue's wait key. Empty on the global
// path, which parks on the coarse wait.
func (e workloadMaterializationResolutionNotReadyError) CorpusWaitRepoIDs() []string {
	return e.repoIDs
}

// CorpusWaitBlockingScopeIDs reports the verdict-time blocking scope set of
// a workload materialization deferral for the queue's wait key. Empty on the
// global path, which parks on the coarse wait.
func (e workloadMaterializationResolutionNotReadyError) CorpusWaitBlockingScopeIDs() []string {
	return e.blockingScopeIDs
}

func (e deployableUnitCorrelationResolutionNotReadyError) Error() string {
	msg := fmt.Sprintf(
		"cross-repo resolution not active for scope %s generation %s; deferring deployable unit correlation rather than evaluating against a partial resolved set",
		e.scopeID,
		e.generationID,
	)
	if e.cause != nil {
		msg += fmt.Sprintf("; corpus fence lookup failed: %v", e.cause)
	}
	if len(e.holdingScopeIDs) > 0 {
		msg += fmt.Sprintf("; holding scopes: %s", strings.Join(e.holdingScopeIDs, ","))
	}
	if e.producerDetail != "" {
		msg += "; " + e.producerDetail
	}
	return msg
}

// Unwrap exposes the fence lookup failure to errors.Is/As without changing
// the retryable failure class.
func (e deployableUnitCorrelationResolutionNotReadyError) Unwrap() error {
	return e.cause
}

func (deployableUnitCorrelationResolutionNotReadyError) Retryable() bool { return true }

// DeployableUnitCorrelationResolutionNotReadyFailureClass classifies a
// deferral of deployable-unit correlation work whose own relationship
// generation has not activated yet.
//
// The storage queue registers this as a non-counting retry class because the
// intent is waiting on an upstream phase, not failing on its own merits.
// Declared beside its FailureClass method (not with the error type) because
// the enrollment guard resolves constants per file: a cross-file constant
// would silently exempt this class from the registry check.
const DeployableUnitCorrelationResolutionNotReadyFailureClass = "deployable_unit_correlation_resolution_not_ready"

func (deployableUnitCorrelationResolutionNotReadyError) FailureClass() string {
	return DeployableUnitCorrelationResolutionNotReadyFailureClass
}

// CorpusWaitRepoIDs reports the narrowed repository set of a deployable-unit
// correlation deferral for the queue's wait key. Empty on the global path.
func (e deployableUnitCorrelationResolutionNotReadyError) CorpusWaitRepoIDs() []string {
	return e.repoIDs
}

// CorpusWaitBlockingScopeIDs reports the verdict-time blocking scope set of
// a deployable-unit correlation deferral for the queue's wait key. Empty on
// the global path.
func (e deployableUnitCorrelationResolutionNotReadyError) CorpusWaitBlockingScopeIDs() []string {
	return e.blockingScopeIDs
}
