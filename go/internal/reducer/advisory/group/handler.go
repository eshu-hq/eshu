// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package group

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/truth"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// WinnersWatermarkNotReadyFailureClass classifies a projection deferred
// because the supply-chain impact canonical winners watermark has not yet
// passed the intent's enqueue time. It is enrolled in the reducer queue's
// non-counting retry classes: the wait is for the winners maintainer, not
// a defect in this intent, so it must not erode the retry budget.
const WinnersWatermarkNotReadyFailureClass = "advisory_group_winners_watermark_not_ready"

// WinnersWatermarkStaleFailureClass classifies the same wait after it has
// exceeded winnersWatermarkMaxWait. It is deliberately not enrolled as
// non-counting: a watermark stuck that long means the maintainer is down,
// and the intent should burn its budget and dead-letter visibly rather
// than wait in silence.
const WinnersWatermarkStaleFailureClass = "advisory_group_winners_watermark_stale"

// winnersWatermarkMaxWait bounds how long one repair cycle defers on the
// winners watermark before the wait starts counting attempts. The
// maintainer ticks on a 30 s cadence, so 30 minutes is 60 missed ticks.
const winnersWatermarkMaxWait = 30 * time.Minute

// WinnersReader is the Postgres winners port the handler reads through.
// cmd/reducer wires the storage/postgres canonical winners store.
type WinnersReader interface {
	// QualifyingAdvisoryGroupWinners returns the canonical winners that
	// currently qualify for one advisory group.
	QualifyingAdvisoryGroupWinners(ctx context.Context, groupID string) ([]WinnerRow, error)
	// AdvisoryGroupWinnersWatermark returns when the canonical winners the
	// reader serves were last rebuilt.
	AdvisoryGroupWinnersWatermark(ctx context.Context) (time.Time, error)
}

// Writer is the graph port the handler writes through. cmd/reducer wires
// the storage/cypher AdvisoryGroupWriter; the reconcile is idempotent per
// group, so a retried intent rewrites the same node and edges.
type Writer interface {
	// ReconcileAdvisoryGroup makes the graph match the group's desired
	// state: its AdvisoryGroup node and AFFECTED_BY edges, or no node at
	// all when the group carries no edges.
	ReconcileAdvisoryGroup(ctx context.Context, group Group) (WriteStats, error)
}

// Definition returns the registration for the advisory_group_projection
// domain. The handler is attached by the reducer root's domain catalog.
//
// The source layers mirror the supply_chain_impact definition, not
// runtime_declaration_projection's source-declaration-only contract: the
// canonical winners this domain projects are derived from that domain's
// findings, which join source-declared manifests and SBOMs with observed
// scanner and runtime evidence, so the projection inherits both layers.
func Definition() reducercontract.DomainDefinition {
	return reducercontract.DomainDefinition{
		Domain:  reducercontract.DomainAdvisoryGroupProjection,
		Summary: "project qualifying supply-chain impact canonical winners into AdvisoryGroup nodes linked from their Repository by AFFECTED_BY",
		Ownership: reducercontract.OwnershipShape{
			CrossSource:    true,
			CrossScope:     true,
			CanonicalWrite: true,
		},
		TruthContract: truth.Contract{
			CanonicalKind: "advisory_group",
			SourceLayers: []truth.Layer{
				truth.LayerSourceDeclaration,
				truth.LayerObservedResource,
			},
		},
	}
}

// Handler projects one advisory group per intent from the supply-chain
// impact canonical winners read model into the graph (#10411).
//
// The intent is enqueued after the impact pass that touched the group
// commits, but the winners read model is rebuilt by a maintainer on its
// own cadence. Handle therefore defers until the winners watermark passes
// the intent's enqueue time: reconciling against winners rebuilt before
// the touching pass committed would project the pre-pass state and then
// acknowledge, leaving the graph stale until the group is touched again.
type Handler struct {
	Winners WinnersReader
	Writer  Writer
	// Instruments records the eshu_dp_advisory_group_projection_* signals.
	// A nil Instruments skips the metrics only.
	Instruments *telemetry.Instruments
	// Now supplies the wall clock for the watermark bound and the lag
	// sample. Nil uses time.Now; tests pin it.
	Now func() time.Time
}

// Handle executes one advisory_group_projection intent.
func (h Handler) Handle(ctx context.Context, intent reducercontract.Intent) (reducercontract.Result, error) {
	if intent.Domain != reducercontract.DomainAdvisoryGroupProjection {
		return reducercontract.Result{}, fmt.Errorf("advisory group handler does not accept domain %q", intent.Domain)
	}
	if h.Winners == nil {
		return reducercontract.Result{}, fmt.Errorf("advisory group winners reader is required")
	}
	if h.Writer == nil {
		return reducercontract.Result{}, fmt.Errorf("advisory group writer is required")
	}
	groupIDs := groupIDsFromIntent(intent)
	if len(groupIDs) == 0 {
		return reducercontract.Result{}, fmt.Errorf("advisory group intent carries no %s entity key", EntityKeyPrefix)
	}

	if err := h.awaitWinnersWatermark(ctx, intent); err != nil {
		return reducercontract.Result{}, err
	}

	domain := string(reducercontract.DomainAdvisoryGroupProjection)
	var totals WriteStats
	winnerRows, oversized := 0, 0
	for _, groupID := range groupIDs {
		rows, err := h.Winners.QualifyingAdvisoryGroupWinners(ctx, groupID)
		if err != nil {
			return reducercontract.Result{}, fmt.Errorf("read qualifying advisory group winners: %w", err)
		}
		winnerRows += len(rows)
		group := BuildGroup(groupID, rows)
		if len(group.CVEIDs)+len(group.AdvisoryIDs) > oversizedMemberUnionThreshold {
			oversized++
			h.Instruments.RecordAdvisoryGroupMemberUnionOversized(ctx, domain)
		}

		reconcileStarted := time.Now()
		stats, err := h.Writer.ReconcileAdvisoryGroup(ctx, group)
		h.Instruments.RecordAdvisoryGroupReconcileDuration(ctx, domain, time.Since(reconcileStarted).Seconds())
		if err != nil {
			h.Instruments.RecordAdvisoryGroupReconciled(ctx, domain, telemetry.AdvisoryGroupProjectionOutcomeFailed, 0, 0, 0)
			return reducercontract.Result{}, fmt.Errorf("reconcile advisory group: %w", err)
		}
		outcome := telemetry.AdvisoryGroupProjectionOutcomeProjected
		if len(group.Edges) == 0 {
			outcome = telemetry.AdvisoryGroupProjectionOutcomeDeleted
		}
		h.Instruments.RecordAdvisoryGroupReconciled(ctx, domain, outcome, stats.EdgesCreated, stats.EdgesDeleted, stats.NodesDeleted)
		totals.NodesCreated += stats.NodesCreated
		totals.NodesDeleted += stats.NodesDeleted
		totals.EdgesCreated += stats.EdgesCreated
		totals.EdgesDeleted += stats.EdgesDeleted
		totals.PropertiesSet += stats.PropertiesSet
	}

	h.Instruments.RecordAdvisoryGroupLag(ctx, domain, h.now().Sub(intent.EnqueuedAt).Seconds())

	slog.InfoContext(
		ctx, "advisory group projection completed",
		log.ScopeID(intent.ScopeID),
		log.GenerationID(intent.GenerationID),
		log.Domain(domain),
		slog.Int("group_count", len(groupIDs)),
		slog.Int("winner_row_count", winnerRows),
		slog.Int("oversized_member_union_count", oversized),
		slog.Int("nodes_created", totals.NodesCreated),
		slog.Int("nodes_deleted", totals.NodesDeleted),
		slog.Int("edges_created", totals.EdgesCreated),
		slog.Int("edges_deleted", totals.EdgesDeleted),
		slog.Int("properties_set", totals.PropertiesSet),
	)

	return reducercontract.Result{
		IntentID: intent.IntentID,
		Domain:   reducercontract.DomainAdvisoryGroupProjection,
		Status:   reducercontract.ResultStatusSucceeded,
		EvidenceSummary: fmt.Sprintf(
			"projected %d advisory groups from %d qualifying winners (edges_created=%d edges_deleted=%d nodes_deleted=%d)",
			len(groupIDs), winnerRows, totals.EdgesCreated, totals.EdgesDeleted, totals.NodesDeleted,
		),
		// CanonicalWrites counts the graph elements this pass created.
		// Deletions converge the cache but are not new canonical truth, so
		// they stay out, matching the per-row write counting of sibling
		// projection handlers.
		CanonicalWrites: totals.NodesCreated + totals.EdgesCreated,
		SubSignals: map[string]float64{
			"groups_projected":             float64(len(groupIDs)),
			"winner_rows":                  float64(winnerRows),
			"edges_created":                float64(totals.EdgesCreated),
			"edges_deleted":                float64(totals.EdgesDeleted),
			"nodes_deleted":                float64(totals.NodesDeleted),
			"oversized_member_union_count": float64(oversized),
		},
	}, nil
}

func (h Handler) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// awaitWinnersWatermark defers the intent until the canonical winners
// were rebuilt at or after the moment this repair cycle was enqueued. The
// bound is the later of the enqueue and cycle-start timestamps: they are
// equal for a fresh row, and a reopened row must wait for winners that
// cover the pass that reopened it, not the original enqueue.
func (h Handler) awaitWinnersWatermark(ctx context.Context, intent reducercontract.Intent) error {
	watermark, err := h.Winners.AdvisoryGroupWinnersWatermark(ctx)
	if err != nil {
		return fmt.Errorf("read advisory group winners watermark: %w", err)
	}
	required := intent.EnqueuedAt
	if intent.CycleStartedAt.After(required) {
		required = intent.CycleStartedAt
	}
	if !watermark.Before(required) {
		return nil
	}
	cycleStart := intent.CycleStartedAt
	if cycleStart.IsZero() {
		cycleStart = intent.EnqueuedAt
	}
	return winnersWatermarkWaitError{
		watermark: watermark,
		required:  required,
		stale:     h.now().Sub(cycleStart) > winnersWatermarkMaxWait,
	}
}

// groupIDsFromIntent extracts the sorted, de-duplicated advisory group
// ids an intent names. Production intents carry exactly one; the sort
// only fixes the reconcile order for a hand-built multi-key intent.
func groupIDsFromIntent(intent reducercontract.Intent) []string {
	seen := make(map[string]struct{}, len(intent.EntityKeys))
	ids := make([]string, 0, len(intent.EntityKeys))
	for _, key := range intent.EntityKeys {
		groupID, ok := GroupIDFromEntityKey(key)
		if !ok {
			continue
		}
		if _, dup := seen[groupID]; dup {
			continue
		}
		seen[groupID] = struct{}{}
		ids = append(ids, groupID)
	}
	sort.Strings(ids)
	return ids
}

// winnersWatermarkWaitError defers a projection until the winners
// maintainer rebuilds past the intent's enqueue time. Inside the bounded
// wait it is an expected wait in the non-counting failure class; past the
// bound it stays retryable but counts attempts, so a dead maintainer
// eventually dead-letters the intent instead of hiding behind the wait.
type winnersWatermarkWaitError struct {
	watermark time.Time
	required  time.Time
	stale     bool
}

func (e winnersWatermarkWaitError) Error() string {
	return fmt.Sprintf(
		"advisory group projection waiting for supply chain impact winners rebuilt at %s to reach the intent enqueue time %s",
		e.watermark.UTC().Format(time.RFC3339), e.required.UTC().Format(time.RFC3339),
	)
}

// Retryable reports that the deferral must re-enter the durable queue.
func (winnersWatermarkWaitError) Retryable() bool { return true }

// ExpectedWait marks the bounded deferral as a designed wait, so the
// reducer records it as deferred at WARN, not failed at ERROR (#8826).
func (e winnersWatermarkWaitError) ExpectedWait() bool { return !e.stale }

// FailureClass returns the queue failure class: the enrolled
// non-counting class inside the bound, the counting stale class past it.
func (e winnersWatermarkWaitError) FailureClass() string {
	if e.stale {
		return WinnersWatermarkStaleFailureClass
	}
	return WinnersWatermarkNotReadyFailureClass
}
