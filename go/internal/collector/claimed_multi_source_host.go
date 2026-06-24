// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// resolveClaimedSource returns the claim-aware source adapter for one
// dispatcher-selected claim target. With a SourceResolver it resolves per target
// (collector kind and instance id), falling back to the single Source only when
// the resolver has no entry; with no resolver it always returns the single
// Source. A non-nil error means the dispatcher selected a target with no source,
// which is a wiring invariant violation (the host builds its dispatch candidates
// from the same registrations).
func (s ClaimedService) resolveClaimedSource(target workflow.ClaimTarget) (ClaimedSource, error) {
	if s.SourceResolver != nil {
		if src, ok := s.SourceResolver(target); ok && src != nil {
			return src, nil
		}
		if s.Source != nil {
			return s.Source, nil
		}
		return nil, fmt.Errorf(
			"no claim-aware source registered for target %s/%s",
			target.CollectorKind, target.CollectorInstanceID,
		)
	}
	return s.Source, nil
}

// ClaimSourceRegistration binds a claim-aware source adapter to the collector
// kind and instance it serves. CollectorInstanceID may be empty to register one
// source for every instance of the kind; an explicit instance id takes
// precedence over a kind-wildcard registration.
type ClaimSourceRegistration struct {
	CollectorKind       scope.CollectorKind
	CollectorInstanceID string
	Source              ClaimedSource
}

// MultiSourceCollectorHost runs multiple claim-aware source adapters behind one
// shared fair claim dispatcher so collector-family fairness is exercised across
// source families and instances inside a single runtime. It owns no claim
// mutations: every worker is a collector.ClaimedService, which remains the sole
// owner of heartbeat, fenced commit, retry, terminal failure, release, and
// completion. The dispatcher (and its FamilyFairnessScheduler) is shared and
// concurrency safe; the source resolver is read-only after construction.
type MultiSourceCollectorHost struct {
	dispatcher *FairClaimDispatcher
	resolver   *hostSourceResolver
	template   ClaimedService
	workers    int
}

// MultiSourceCollectorHostConfig configures a multi-source collector host.
// Sources binds each claim-aware (collector kind, instance) to its source
// adapter; Instances is the durable collector instance state that seeds the
// fair dispatch candidates (disabled or claims-disabled rows are filtered out
// before dispatch). Template carries the shared claim lifecycle configuration
// (control store, committer, owner id, claim id function, intervals, retry
// budget, telemetry) applied to every worker; its Source/SourceResolver/
// ClaimDispatcher fields are overridden by the host. Workers is the concurrent
// worker count (defaults to 1).
//
// Template.ClaimIDFunc MUST return a globally unique claim id on every call and
// MUST be safe for concurrent invocation: the host shares it across all workers,
// and two workers that obtain the same claim id while claiming different work
// items would corrupt claim-fence identity. Wire it to a UUID generator (or an
// equivalent collision-free, concurrency-safe source), never a static or
// unsynchronized value.
type MultiSourceCollectorHostConfig struct {
	Sources   []ClaimSourceRegistration
	Instances []workflow.CollectorInstance
	Template  ClaimedService
	Workers   int
}

type hostTargetKey struct {
	kind     scope.CollectorKind
	instance string
}

// hostSourceResolver resolves a claim-aware source for a dispatched target,
// preferring an exact (kind, instance) registration over a kind-wildcard one.
type hostSourceResolver struct {
	byTarget map[hostTargetKey]ClaimedSource
	byKind   map[scope.CollectorKind]ClaimedSource
}

func (r *hostSourceResolver) resolve(target workflow.ClaimTarget) (ClaimedSource, bool) {
	if src, ok := r.byTarget[hostTargetKey{target.CollectorKind, target.CollectorInstanceID}]; ok {
		return src, true
	}
	src, ok := r.byKind[target.CollectorKind]
	return src, ok
}

// NewMultiSourceCollectorHost validates the configuration, filters the durable
// instance state to the claim-enabled dispatch candidates, requires a registered
// source for every candidate, builds the shared fair dispatcher, and returns a
// runnable host. Disabled or claims-disabled instances are skipped before the
// source requirement so a host never fails to start merely because the control
// table holds a non-dispatchable registration for another collector family.
func NewMultiSourceCollectorHost(cfg MultiSourceCollectorHostConfig) (*MultiSourceCollectorHost, error) {
	if cfg.Template.ControlStore == nil {
		return nil, errors.New("claim control store is required")
	}
	if len(cfg.Sources) == 0 {
		return nil, errors.New("at least one claim-aware source registration is required")
	}
	if len(cfg.Instances) == 0 {
		return nil, errors.New("at least one collector instance is required")
	}

	resolver, err := buildHostSourceResolver(cfg.Sources)
	if err != nil {
		return nil, err
	}

	candidates, err := workflow.FairnessCandidatesFromCollectorInstances(cfg.Instances)
	if err != nil {
		return nil, fmt.Errorf("build dispatch candidates: %w", err)
	}
	if len(candidates) == 0 {
		return nil, errors.New("no claim-enabled collector instances to dispatch")
	}
	for _, candidate := range candidates {
		target := workflow.ClaimTarget{
			CollectorKind:       candidate.CollectorKind,
			CollectorInstanceID: candidate.CollectorInstanceID,
		}
		if _, ok := resolver.resolve(target); !ok {
			return nil, fmt.Errorf(
				"dispatch candidate %s/%s has no registered source",
				candidate.CollectorKind, candidate.CollectorInstanceID,
			)
		}
	}

	dispatcher, err := NewFairClaimDispatcher(cfg.Template.ControlStore, candidates)
	if err != nil {
		return nil, fmt.Errorf("build fair claim dispatcher: %w", err)
	}

	workers := cfg.Workers
	if workers < 1 {
		workers = 1
	}
	return &MultiSourceCollectorHost{
		dispatcher: dispatcher,
		resolver:   resolver,
		template:   cfg.Template,
		workers:    workers,
	}, nil
}

func buildHostSourceResolver(registrations []ClaimSourceRegistration) (*hostSourceResolver, error) {
	resolver := &hostSourceResolver{
		byTarget: make(map[hostTargetKey]ClaimedSource),
		byKind:   make(map[scope.CollectorKind]ClaimedSource),
	}
	for _, registration := range registrations {
		if registration.Source == nil {
			return nil, fmt.Errorf("claim-aware source for kind %q is nil", registration.CollectorKind)
		}
		if registration.CollectorInstanceID == "" {
			if _, dup := resolver.byKind[registration.CollectorKind]; dup {
				return nil, fmt.Errorf("duplicate kind-wildcard source for kind %q", registration.CollectorKind)
			}
			resolver.byKind[registration.CollectorKind] = registration.Source
			continue
		}
		key := hostTargetKey{registration.CollectorKind, registration.CollectorInstanceID}
		if _, dup := resolver.byTarget[key]; dup {
			return nil, fmt.Errorf(
				"duplicate source for %s/%s",
				registration.CollectorKind, registration.CollectorInstanceID,
			)
		}
		resolver.byTarget[key] = registration.Source
	}
	return resolver, nil
}

// Run drives the configured number of concurrent workers, each a ClaimedService
// that claims through the shared fair dispatcher and resolves the matching
// source per dispatched target. It returns when the context is cancelled or the
// first worker returns a non-nil error; the remaining workers are then
// cancelled. Per-instance FIFO ordering inside a selected claim target is
// unchanged because each claim still flows through ClaimNextEligible.
func (h *MultiSourceCollectorHost) Run(ctx context.Context) error {
	if h == nil {
		return errors.New("multi-source collector host is required")
	}

	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg     sync.WaitGroup
		once   sync.Once
		runErr error
	)
	for i := 0; i < h.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker := h.template
			worker.ClaimDispatcher = h.dispatcher
			worker.SourceResolver = h.resolver.resolve
			worker.Source = nil
			worker.CollectorKind = ""
			worker.CollectorInstanceID = ""
			if err := worker.Run(workerCtx); err != nil {
				once.Do(func() {
					runErr = err
					cancel()
				})
			}
		}()
	}
	wg.Wait()
	return runErr
}
