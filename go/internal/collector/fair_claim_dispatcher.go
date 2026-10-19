// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ClaimDispatcher chooses the next collector target that should attempt a
// durable workflow claim.
type ClaimDispatcher interface {
	ClaimNext(context.Context, string, string, time.Time, time.Duration) (workflow.WorkItem, workflow.Claim, workflow.ClaimTarget, bool, error)
}

// ClaimPacingChecker is the depth-pacing consult surface the dispatcher
// paces grants through. WorkflowControlStore implements it; scripted fakes
// serve the dispatcher proofs.
type ClaimPacingChecker interface {
	CheckClaimPacing(context.Context, workflow.ClaimSelector, workflow.ClaimPacingConfig, time.Time) (workflow.ClaimPacingDecision, error)
}

// FairClaimDispatcher applies workflow family fairness before delegating each
// selected target to the existing fenced ClaimNextEligible store operation.
type FairClaimDispatcher struct {
	controlStore ClaimControlStore
	scheduler    *workflow.FamilyFairnessScheduler
	targetCount  int
	mu           sync.Mutex
	// pacingChecker, when set with an enabled pacingConfig, paces grants on
	// per-domain depth first with global depth as the ceiling. A paced
	// target is skipped work-conservingly: the dispatcher tries the next
	// family instead of sleeping. Nil (the default) claims with no pacing
	// consult.
	pacingChecker ClaimPacingChecker
	pacingConfig  workflow.ClaimPacingConfig
}

// WithClaimPacing paces the dispatcher's grants through checker under cfg
// and returns the dispatcher for chaining. A nil checker or a disabled
// config leaves pacing off.
func (d *FairClaimDispatcher) WithClaimPacing(
	checker ClaimPacingChecker,
	cfg workflow.ClaimPacingConfig,
) *FairClaimDispatcher {
	if d == nil {
		return nil
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.pacingChecker = checker
	d.pacingConfig = cfg
	return d
}

// NewFairClaimDispatcher builds a family-fair claim dispatcher for eligible
// claim candidates.
func NewFairClaimDispatcher(
	controlStore ClaimControlStore,
	candidates []workflow.FairnessCandidate,
) (*FairClaimDispatcher, error) {
	if controlStore == nil {
		return nil, errors.New("claim control store is required")
	}
	if len(candidates) == 0 {
		return nil, errors.New("claim dispatcher requires at least one claim target")
	}
	scheduler, err := workflow.NewFamilyFairnessScheduler(candidates)
	if err != nil {
		return nil, err
	}
	return &FairClaimDispatcher{
		controlStore: controlStore,
		scheduler:    scheduler,
		targetCount:  len(candidates),
	}, nil
}

// NewFairClaimDispatcherFromInstances builds a family-fair dispatcher from
// durable collector instance state.
func NewFairClaimDispatcherFromInstances(
	controlStore ClaimControlStore,
	instances []workflow.CollectorInstance,
) (*FairClaimDispatcher, error) {
	candidates, err := workflow.FairnessCandidatesFromCollectorInstances(instances)
	if err != nil {
		return nil, err
	}
	return NewFairClaimDispatcher(controlStore, candidates)
}

// ClaimNext advances across a bounded number of scheduler targets, skipping
// empty family lanes without sleeping while another eligible family has work.
func (d *FairClaimDispatcher) ClaimNext(
	ctx context.Context,
	ownerID string,
	claimID string,
	now time.Time,
	leaseTTL time.Duration,
) (workflow.WorkItem, workflow.Claim, workflow.ClaimTarget, bool, error) {
	if d == nil {
		return workflow.WorkItem{}, workflow.Claim{}, workflow.ClaimTarget{}, false, errors.New("claim dispatcher is required")
	}
	ownerID = strings.TrimSpace(ownerID)
	claimID = strings.TrimSpace(claimID)
	if ownerID == "" {
		return workflow.WorkItem{}, workflow.Claim{}, workflow.ClaimTarget{}, false, errors.New("owner id is required")
	}
	if claimID == "" {
		return workflow.WorkItem{}, workflow.Claim{}, workflow.ClaimTarget{}, false, errors.New("claim id is required")
	}
	checker, pacingConfig := d.pacingConsult()
	var deferral *workflow.ClaimPacingDeferral
	for i := 0; i < d.targetCount; i++ {
		target, ok := d.nextTarget()
		if !ok {
			return workflow.WorkItem{}, workflow.Claim{}, workflow.ClaimTarget{}, false, nil
		}
		selector := workflow.ClaimSelector{
			CollectorKind:       target.CollectorKind,
			CollectorInstanceID: target.CollectorInstanceID,
			OwnerID:             ownerID,
			ClaimID:             claimID,
		}
		if checker != nil {
			decision, err := checker.CheckClaimPacing(ctx, selector, pacingConfig, now)
			if err == nil && decision.Paced {
				deferral = pacingLongestDeferral(deferral, decision)
				continue
			}
			// A pacing consult failure fails open: an unavailable depth
			// signal is not evidence of a backlog, so the target claims
			// normally.
		}
		item, claim, found, err := d.controlStore.ClaimNextEligible(ctx, selector, now, leaseTTL)
		if err != nil {
			return workflow.WorkItem{}, workflow.Claim{}, workflow.ClaimTarget{}, false,
				fmt.Errorf("claim next %s/%s work item: %w", target.CollectorKind, target.CollectorInstanceID, err)
		}
		if found {
			return item, claim, target, true, nil
		}
	}
	if deferral != nil {
		return workflow.WorkItem{}, workflow.Claim{}, workflow.ClaimTarget{}, false, deferral
	}
	return workflow.WorkItem{}, workflow.Claim{}, workflow.ClaimTarget{}, false, nil
}

// pacingConsult snapshots the pacing consult pair. A nil checker or a
// disabled config claims with no pacing consult.
func (d *FairClaimDispatcher) pacingConsult() (ClaimPacingChecker, workflow.ClaimPacingConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.pacingChecker == nil || !d.pacingConfig.Enabled() {
		return nil, workflow.ClaimPacingConfig{}
	}
	return d.pacingChecker, d.pacingConfig
}

// pacingLongestDeferral keeps the longest grant delay across paced targets,
// so the runner backs off for the most saturated domain it met.
func pacingLongestDeferral(
	current *workflow.ClaimPacingDeferral,
	decision workflow.ClaimPacingDecision,
) *workflow.ClaimPacingDeferral {
	if current != nil && current.RetryAfter >= decision.RetryAfter {
		return current
	}
	return &workflow.ClaimPacingDeferral{
		Signal:     decision.Signal,
		Domain:     decision.Domain,
		RetryAfter: decision.RetryAfter,
		Stuck:      decision.Stuck,
	}
}

func (d *FairClaimDispatcher) nextTarget() (workflow.ClaimTarget, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.scheduler.Next()
}
