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

// FairClaimDispatcher applies workflow family fairness before delegating each
// selected target to the existing fenced ClaimNextEligible store operation.
type FairClaimDispatcher struct {
	controlStore ClaimControlStore
	scheduler    *workflow.FamilyFairnessScheduler
	targetCount  int
	mu           sync.Mutex
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
	for i := 0; i < d.targetCount; i++ {
		target, ok := d.nextTarget()
		if !ok {
			return workflow.WorkItem{}, workflow.Claim{}, workflow.ClaimTarget{}, false, nil
		}
		item, claim, found, err := d.controlStore.ClaimNextEligible(ctx, workflow.ClaimSelector{
			CollectorKind:       target.CollectorKind,
			CollectorInstanceID: target.CollectorInstanceID,
			OwnerID:             ownerID,
			ClaimID:             claimID,
		}, now, leaseTTL)
		if err != nil {
			return workflow.WorkItem{}, workflow.Claim{}, workflow.ClaimTarget{}, false,
				fmt.Errorf("claim next %s/%s work item: %w", target.CollectorKind, target.CollectorInstanceID, err)
		}
		if found {
			return item, claim, target, true, nil
		}
	}
	return workflow.WorkItem{}, workflow.Claim{}, workflow.ClaimTarget{}, false, nil
}

func (d *FairClaimDispatcher) nextTarget() (workflow.ClaimTarget, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.scheduler.Next()
}
