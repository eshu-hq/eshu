// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossrepo"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/maintenance/accepted"
)

// deferUnlessReadyForBulkLoad evaluates the cheap readiness probes before
// LoadWorkloadProjectionScopeInputs pays for its bulk scope-local fact load
// (#9697): under a resolution storm almost every intent deferred after
// loading, so the load was pure allocation churn feeding the engine OOM
// livelock. A deferral here returns the same non-counting
// workloadMaterializationResolutionNotReadyError (or the same counting
// producer error) the post-load gates return, only earlier. The post-load
// gates still run after the load, so graph truth is unchanged: a ready intent
// loads exactly once, and a deferred intent retries on the same fence.
//
// The pre-gate fires only when the loader's candidate-precondition probe
// explicitly reports the scope generation may hold candidates. A missing
// probe, a probe error, or a false answer all proceed to the normal
// load-then-gate path, so candidate-free scopes keep their pinned vacuous
// success (no bulk load is shed for them, but none is wasted either: the
// normal path succeeds without consulting the fence).
func (l CorrelatedWorkloadProjectionInputLoader) deferUnlessReadyForBulkLoad(
	ctx context.Context,
	intent reducercontract.Intent,
) error {
	prober, ok := l.FactLoader.(factload.WorkloadCandidateProbe)
	if !ok {
		return nil
	}
	mayHold, err := prober.ScopeMayHoldWorkloadCandidates(ctx, intent.ScopeID, intent.GenerationID)
	if err != nil || !mayHold {
		return nil
	}
	// Own-generation probe: a single-key activation read, no bulk load. The
	// full own-gate verdict runs with assumed candidates (the probe just
	// established they may exist) so a dead-lettered or long-missing
	// producer still counts instead of deferring forever (#7597).
	if l.ResolutionActiveLookup != nil {
		active, err := l.ResolutionActiveLookup(intent.GenerationID)
		if err != nil {
			return workloadMaterializationResolutionNotReadyError{
				scopeID: intent.ScopeID, generationID: intent.GenerationID,
			}
		}
		if !active {
			gate := crossrepo.EvaluateOwnGate(ctx, l.ResolutionActiveLookup, l.ResolutionProducerLookup,
				intent, true, time.Now().UTC())
			if gate.Err != nil {
				return gate.Err
			}
			return workloadMaterializationResolutionNotReadyError{
				scopeID: intent.ScopeID, generationID: intent.GenerationID, producerDetail: gate.ProducerDetail,
			}
		}
	}
	// Corpus-wide probe: one aggregate read, no bulk load. Skipped for fused
	// loaders, whose verdict comes from the by-repos read's own statement
	// snapshot (#6740) and for which ResolutionsCompleteLookup stays
	// unconsumed by contract.
	if _, fused := l.ResolvedLoader.(CorpusFencedResolvedRelationshipLoader); !fused &&
		l.ResolutionsCompleteLookup != nil {
		complete, err := l.ResolutionsCompleteLookup(ctx)
		if err != nil {
			return workloadMaterializationResolutionNotReadyError{
				scopeID: intent.ScopeID, generationID: intent.GenerationID, cause: err,
			}
		}
		if !complete {
			return workloadMaterializationResolutionNotReadyError{
				scopeID:         intent.ScopeID,
				generationID:    intent.GenerationID,
				holdingScopeIDs: accepted.IncompleteScopeIDs(ctx, l.IncompleteScopesLookup),
			}
		}
	}
	return nil
}
