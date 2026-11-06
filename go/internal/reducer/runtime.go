// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// GenerationFreshnessCheck reports whether the given generation is still
// the active generation for the scope. Returns (true, nil) if current,
// (false, nil) if superseded, or (false, err) on lookup failure.
type GenerationFreshnessCheck = reducercontract.GenerationFreshnessCheck

// ActiveGenerationID returns the scope's currently active generation id,
// reporting found=false when the scope is unknown or has no active
// generation. It backs the superseded-intent rebind in execute (#10106).
type ActiveGenerationID = reducercontract.ActiveGenerationID

// PriorGenerationCheck reports whether the scope has a generation strictly
// older than the given generation in activation order, so the answer does not
// depend on queue timing (#8167). Retract paths use it to skip no-op cleanup
// on first writes while preserving cleanup on refreshes and retries.
type PriorGenerationCheck = reducercontract.PriorGenerationCheck

// PriorGenerationID returns the scope's generation immediately before the
// given generation. Generation diff retracts (#6887) enumerate delete
// candidates from the predecessor's facts.
type PriorGenerationID = reducercontract.PriorGenerationID

// RunReport summarizes one bounded reducer drain.
type RunReport struct {
	StartedAt  time.Time
	FinishedAt time.Time
	Claimed    int
	Processed  int
	Succeeded  int
	Failed     int
}

// DomainStats summarizes runtime state for one reducer domain.
type DomainStats struct {
	Domain    reducercontract.Domain
	Summary   string
	Ownership OwnershipShape
	Pending   int
	Claimed   int
	Running   int
	Succeeded int
	Failed    int
}

// Stats captures the reducer runtime backlog and execution shape.
type Stats struct {
	Queued    int
	Pending   int
	Claimed   int
	Running   int
	Succeeded int
	Failed    int
	Domains   []DomainStats
}

// Runtime owns the bounded reducer execution surface.
type Runtime struct {
	mu              sync.Mutex
	registry        Registry
	intents         []reducercontract.Intent
	GenerationCheck GenerationFreshnessCheck // nil disables the guard
	// ActiveGeneration resolves a scope's active generation for the
	// superseded-intent rebind in execute (#10106). Nil disables the
	// rebind: superseded intents keep the terminal superseded result.
	ActiveGeneration ActiveGenerationID
}

// NewRuntime constructs a reducer runtime over the supplied registry.
func NewRuntime(registry Registry) (*Runtime, error) {
	if registry.defs == nil {
		registry.defs = make(map[reducercontract.Domain]DomainDefinition)
	}

	return &Runtime{registry: registry}, nil
}

// Enqueue persists one reducer intent in the bounded runtime queue.
func (r *Runtime) Enqueue(now time.Time, intent reducercontract.Intent) (reducercontract.Intent, error) {
	if now.IsZero() {
		return reducercontract.Intent{}, fmt.Errorf("now must not be zero")
	}

	queued := intent.Clone()
	if queued.EnqueuedAt.IsZero() {
		queued.EnqueuedAt = now
	}
	if queued.AvailableAt.IsZero() {
		queued.AvailableAt = queued.EnqueuedAt
	}
	if queued.Status == "" {
		queued.Status = IntentStatusPending
	}

	if err := queued.Validate(); err != nil {
		return reducercontract.Intent{}, err
	}
	if _, ok := r.registry.Definition(queued.Domain); !ok {
		return reducercontract.Intent{}, fmt.Errorf("domain %q is not registered", queued.Domain)
	}
	if queued.Status != IntentStatusPending {
		return reducercontract.Intent{}, fmt.Errorf("intent must enqueue as pending, got %q", queued.Status)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.intents = append(r.intents, queued)

	return queued.Clone(), nil
}

// RunOnce drains up to max ready intents and executes them.
func (r *Runtime) RunOnce(ctx context.Context, now time.Time, max int) (RunReport, error) {
	if now.IsZero() {
		return RunReport{}, fmt.Errorf("now must not be zero")
	}
	if max <= 0 {
		return RunReport{}, fmt.Errorf("max must be positive")
	}

	startedAt := now
	positions, intents := r.claimBatch(now, max)
	report := RunReport{StartedAt: startedAt, FinishedAt: startedAt}
	if len(intents) == 0 {
		return report, nil
	}
	report.Claimed = len(intents)

	for idx, intent := range intents {
		position := positions[idx]
		if err := ctx.Err(); err != nil {
			r.markFailed(position, now, err.Error(), "context canceled")
			report.Failed++
			report.Processed++
			continue
		}

		r.markRunning(position)
		result, err := r.Execute(ctx, intent)
		if err != nil {
			r.markFailed(position, now, "execution failed", err.Error())
			report.Failed++
			report.Processed++
			continue
		}
		if result.Status == "" {
			result.Status = ResultStatusSucceeded
		}
		if result.CompletedAt.IsZero() {
			result.CompletedAt = now
		}
		if result.IntentID == "" {
			result.IntentID = intent.IntentID
		}
		if result.Domain == "" {
			result.Domain = intent.Domain
		}

		if result.Status == ResultStatusFailed {
			r.markFailed(position, result.CompletedAt, "execution reported failure", result.EvidenceSummary)
			report.Failed++
			report.Processed++
			continue
		}

		r.markSucceeded(position, result.CompletedAt, result)
		report.Succeeded++
		report.Processed++
	}

	report.FinishedAt = now

	return report, nil
}

// Execute runs one reducer intent through its registered handler.
func (r *Runtime) Execute(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	return r.execute(ctx, intent)
}

// Stats snapshots the queue and domain state.
func (r *Runtime) Stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()

	stats := Stats{}
	for _, intent := range r.intents {
		switch intent.Status {
		case IntentStatusPending:
			stats.Pending++
			stats.Queued++
		case IntentStatusClaimed:
			stats.Claimed++
			stats.Queued++
		case IntentStatusRunning:
			stats.Running++
			stats.Queued++
		case IntentStatusSucceeded:
			stats.Succeeded++
		case IntentStatusFailed:
			stats.Failed++
		}
	}

	for _, def := range r.registry.Definitions() {
		domainStats := DomainStats{
			Domain:    def.Domain,
			Summary:   def.Summary,
			Ownership: def.Ownership,
		}
		for _, intent := range r.intents {
			if intent.Domain != def.Domain {
				continue
			}
			switch intent.Status {
			case IntentStatusPending:
				domainStats.Pending++
			case IntentStatusClaimed:
				domainStats.Claimed++
			case IntentStatusRunning:
				domainStats.Running++
			case IntentStatusSucceeded:
				domainStats.Succeeded++
			case IntentStatusFailed:
				domainStats.Failed++
			}
		}
		stats.Domains = append(stats.Domains, domainStats)
	}

	sort.SliceStable(stats.Domains, func(i, j int) bool {
		return stats.Domains[i].Domain < stats.Domains[j].Domain
	})

	return stats
}

func (r *Runtime) claimBatch(now time.Time, max int) ([]int, []reducercontract.Intent) {
	r.mu.Lock()
	defer r.mu.Unlock()

	type candidate struct {
		index  int
		intent reducercontract.Intent
	}

	candidates := make([]candidate, 0, len(r.intents))
	for idx, intent := range r.intents {
		if intent.Status != IntentStatusPending {
			continue
		}
		if intent.AvailableAt.After(now) {
			continue
		}
		candidates = append(candidates, candidate{index: idx, intent: intent.Clone()})
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left := candidates[i].intent
		right := candidates[j].intent
		if left.Priority != right.Priority {
			return left.Priority > right.Priority
		}
		if !left.AvailableAt.Equal(right.AvailableAt) {
			return left.AvailableAt.Before(right.AvailableAt)
		}
		if !left.EnqueuedAt.Equal(right.EnqueuedAt) {
			return left.EnqueuedAt.Before(right.EnqueuedAt)
		}
		if left.IntentID != right.IntentID {
			return left.IntentID < right.IntentID
		}
		return candidates[i].index < candidates[j].index
	})

	if len(candidates) > max {
		candidates = candidates[:max]
	}

	positions := make([]int, 0, len(candidates))
	intents := make([]reducercontract.Intent, 0, len(candidates))
	for _, candidate := range candidates {
		positions = append(positions, candidate.index)
		claimed := r.intents[candidate.index].Clone()
		claimed.Status = IntentStatusClaimed
		claimed.ClaimedAt = &now
		r.intents[candidate.index] = claimed.Clone()
		intents = append(intents, claimed.Clone())
	}

	return positions, intents
}

func (r *Runtime) markRunning(position int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if position < 0 || position >= len(r.intents) {
		return
	}

	intent := r.intents[position].Clone()
	intent.Status = IntentStatusRunning
	r.intents[position] = intent
}

func (r *Runtime) markSucceeded(position int, completedAt time.Time, result reducercontract.Result) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if position < 0 || position >= len(r.intents) {
		return
	}

	intent := r.intents[position].Clone()
	intent.Status = IntentStatusSucceeded
	intent.CompletedAt = &completedAt
	intent.Failure = nil
	r.intents[position] = intent
}

func (r *Runtime) markFailed(position int, completedAt time.Time, failureClass, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if position < 0 || position >= len(r.intents) {
		return
	}

	intent := r.intents[position].Clone()
	intent.Status = IntentStatusFailed
	intent.CompletedAt = &completedAt
	intent.Failure = &FailureRecord{
		FailureClass: failureClass,
		Message:      message,
	}
	r.intents[position] = intent
}

func (r *Runtime) execute(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	// Generation guard: skip stale intents before touching Neo4j.
	reboundFrom := ""
	if r.GenerationCheck != nil {
		current, from, err := r.generationCurrent(ctx, &intent)
		if err != nil {
			return reducercontract.Result{}, err
		}
		reboundFrom = from
		if !current {
			return reducercontract.Result{
				IntentID:        intent.IntentID,
				Domain:          intent.Domain,
				Status:          ResultStatusSuperseded,
				EvidenceSummary: fmt.Sprintf("generation %s superseded for scope %s", intent.GenerationID, intent.ScopeID),
				CanonicalWrites: 0,
				CompletedAt:     time.Now(),
			}, nil
		}
	}

	def, ok := r.registry.Definition(intent.Domain)
	if !ok {
		return reducercontract.Result{}, fmt.Errorf("domain %q is not registered", intent.Domain)
	}
	if def.Handler == nil {
		return reducercontract.Result{}, fmt.Errorf("domain %q has no handler", intent.Domain)
	}

	result, err := def.Handler.Handle(ctx, intent)
	if err == nil && reboundFrom != "" {
		note := fmt.Sprintf(
			"rebound from superseded generation %s to active generation %s (#10106)",
			reboundFrom, intent.GenerationID,
		)
		if result.EvidenceSummary != "" {
			result.EvidenceSummary += "; " + note
		} else {
			result.EvidenceSummary = note
		}
	}
	return result, err
}

// maxGenerationRebounds bounds how many times one execute rebinds a
// scope-keyed intent onto its scope's newly active generation (#10106).
// Each rebound consumes a real activation that committed inside the window
// between two primary-key reads, so exhausting the budget means sustained
// activation churn, never a hot loop.
const maxGenerationRebounds = 3

// generationCurrent reports whether intent may run against its (possibly
// rebound) generation. A superseded Crossplane SATISFIED_BY intent rebinds
// to the scope's active generation instead of dropping (#10106): that
// domain's intent is keyed per scope (its EntityKey names only the scope)
// and its handler materializes the scope's active snapshot — claims from
// the active generation overlaid on its delta base, resolved against every
// active XRD — so running it under the successor generation computes
// exactly what the successor's own intent would. A quiet delta derives no
// such intent of its own (the projector builds intents only from a
// generation's own facts), so without the rebind a replay scheduled
// against the predecessor is lost until an unrelated future generation.
// Every other domain's intent carries generation-scoped facts and keeps
// the terminal superseded result. reboundFrom names the generation the
// intent was scheduled against when a rebind happened, "" otherwise.
func (r *Runtime) generationCurrent(
	ctx context.Context,
	intent *reducercontract.Intent,
) (current bool, reboundFrom string, err error) {
	for rebounds := 0; ; rebounds++ {
		current, err := r.GenerationCheck(ctx, intent.ScopeID, intent.GenerationID)
		if err != nil {
			return false, reboundFrom, fmt.Errorf("generation freshness check: %w", err)
		}
		if current {
			return true, reboundFrom, nil
		}
		if r.ActiveGeneration == nil || intent.Domain != reducercontract.DomainCrossplaneSatisfiedByMaterialization {
			return false, reboundFrom, nil
		}
		if rebounds >= maxGenerationRebounds {
			active, _, _ := r.ActiveGeneration(ctx, intent.ScopeID)
			return false, reboundFrom, reducercontract.GenerationRebindExhaustedError{
				ScopeID:            intent.ScopeID,
				GenerationID:       intent.GenerationID,
				ActiveGenerationID: strings.TrimSpace(active),
				Rebounds:           rebounds,
			}
		}
		active, found, err := r.ActiveGeneration(ctx, intent.ScopeID)
		if err != nil {
			return false, reboundFrom, fmt.Errorf("active generation lookup: %w", err)
		}
		active = strings.TrimSpace(active)
		if !found || active == "" || active == intent.GenerationID {
			// No successor to carry the work to: the scope has no active
			// generation, or the lookup and the freshness check raced one
			// activation apart and already agree. The superseded result
			// stands, exactly as before the rebind existed.
			return false, reboundFrom, nil
		}
		if reboundFrom == "" {
			reboundFrom = intent.GenerationID
		}
		intent.GenerationID = active
	}
}
