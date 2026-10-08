// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// collectorKind is the collector_kind label and log value of every signal
// this package emits; only the git collector evaluates selection.
const collectorKind = "git"

// Closed failure classes of the git_repository_selection_store_failed log.
const (
	// FailureClassKnownScopesRead means reading the org's known scopes failed.
	FailureClassKnownScopesRead = "known_scopes_read"
	// FailureClassObservationsRead means reading the selector's prior
	// observations failed.
	FailureClassObservationsRead = "observations_read"
	// FailureClassUpsert means the batched observation upsert failed.
	FailureClassUpsert = "upsert"
	// FailureClassStoreMissing means the observer was wired without a store.
	FailureClassStoreMissing = "store_missing"
)

// Store persists selection observations. KnownScopes and Observations are
// plain reads that take no row locks; UpsertObservations writes one batch in
// one statement and only advances rows older than the batch.
type Store interface {
	// KnownScopes returns the git default-branch repository scopes whose
	// stored repo slug belongs to owner, case-insensitively.
	KnownScopes(ctx context.Context, owner string) ([]KnownScope, error)
	// Observations returns every stored observation for selectorID.
	Observations(ctx context.Context, selectorID string) ([]Observation, error)
	// UpsertObservations writes batch.
	UpsertObservations(ctx context.Context, batch Batch) error
}

// Request is one cycle's evaluation request from the git collector.
type Request struct {
	Selector       Selector
	SourceMode     string
	RepoShardCount int
	RepoLimit      int
	Now            time.Time
	Listing        Listing
}

// Observer evaluates one selector per cycle and records the outcome. It never
// returns an error: a store failure is logged, counted as store_error, and the
// collector cycle continues with today's behavior.
type Observer struct {
	Store Store
	// MinimumInterval is the evaluation interval floor; zero means
	// DefaultMinimumInterval.
	MinimumInterval time.Duration
	Instruments     *telemetry.Instruments
	Logger          *slog.Logger
}

// Observe evaluates req, writes the batch when the evaluation passes every
// rail, and emits the evaluation counter, the scope gauge (evaluated cycles
// only), and the git_repository_selection_* logs.
func (o Observer) Observe(ctx context.Context, req Request) Result {
	result, failureClass, err := o.evaluate(ctx, req)
	if failureClass != "" {
		result.Outcome = OutcomeStoreError
		o.warn(ctx, "git_repository_selection_store_failed",
			slog.String("selector_id", req.Selector.ID),
			log.FailureClass(failureClass),
			log.Err(err),
		)
	}
	o.record(ctx, result)
	o.logEvaluated(ctx, req, result)
	return result
}

func (o Observer) evaluate(ctx context.Context, req Request) (Result, string, error) {
	in := Input{Selector: req.Selector, Now: req.Now, MinimumInterval: o.MinimumInterval, Listing: req.Listing}
	if !req.Listing.Complete {
		result := Evaluate(in)
		o.warn(ctx, "git_repository_selection_listing_truncated",
			slog.String("selector_id", req.Selector.ID),
			slog.Int("repo_limit", req.RepoLimit),
			slog.Int("listed_count", result.Counts.Listed),
		)
		return result, "", nil
	}
	var listingOnly Result
	indexListing(req.Listing.Repositories, &listingOnly.Counts)
	if o.Store == nil {
		return listingOnly, FailureClassStoreMissing, errors.New("repository selection store is not configured")
	}
	known, err := o.Store.KnownScopes(ctx, req.Selector.Owner)
	if err != nil {
		return listingOnly, FailureClassKnownScopesRead, fmt.Errorf("read known repository scopes for org %q: %w", req.Selector.Owner, err)
	}
	prior, err := o.Store.Observations(ctx, req.Selector.ID)
	if err != nil {
		return listingOnly, FailureClassObservationsRead, fmt.Errorf("read repository selection observations for selector %s: %w", req.Selector.ID, err)
	}
	in.Known, in.Prior = known, prior
	result := Evaluate(in)
	switch result.Outcome {
	case OutcomeGuardTripped:
		o.warn(ctx, "git_repository_selection_guard_tripped",
			slog.String("selector_id", req.Selector.ID),
			slog.Int("listed_count", result.Counts.Listed),
			slog.Int("known_scope_count", result.Counts.Known),
			slog.Int("newly_unlisted_count", result.Counts.NewlyUnlisted),
			slog.Int("guard_threshold", result.GuardThreshold),
		)
	case OutcomeEvaluated:
		if err := o.Store.UpsertObservations(ctx, result.Batch); err != nil {
			return result, FailureClassUpsert, fmt.Errorf("upsert %d repository selection observations: %w", len(result.Batch.Rows), err)
		}
	case OutcomeListingTruncated, OutcomeStoreError:
		// Unreachable: a truncated listing returned above, and Evaluate never
		// reports a store error.
	}
	return result, "", nil
}

func (o Observer) record(ctx context.Context, result Result) {
	if o.Instruments == nil {
		return
	}
	if o.Instruments.RepositorySelectionEvaluations != nil {
		o.Instruments.RepositorySelectionEvaluations.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrCollectorKind(collectorKind),
			telemetry.AttrOutcome(string(result.Outcome)),
		))
	}
	if result.Outcome != OutcomeEvaluated || o.Instruments.RepositorySelectionScopes == nil {
		return
	}
	for state, value := range map[string]int{
		telemetry.RepositorySelectionStateSelected:         result.Scopes.Selected,
		telemetry.RepositorySelectionStateNotListedPending: result.Scopes.NotListedPending,
		telemetry.RepositorySelectionStateNotListed:        result.Scopes.NotListed,
		telemetry.RepositorySelectionStateArchivedExcluded: result.Scopes.ArchivedExcluded,
		telemetry.RepositorySelectionStateRuleExcluded:     result.Scopes.RuleExcluded,
	} {
		o.Instruments.RepositorySelectionScopes.Record(ctx, int64(value), metric.WithAttributes(
			telemetry.AttrCollectorKind(collectorKind),
			telemetry.AttrState(state),
		))
	}
}

func (o Observer) logEvaluated(ctx context.Context, req Request, result Result) {
	if o.Logger == nil {
		return
	}
	o.Logger.InfoContext(ctx, "git_repository_selection_evaluated",
		log.CollectorKind(collectorKind),
		slog.String("selector_id", req.Selector.ID),
		slog.String("source_mode", req.SourceMode),
		slog.Int("repo_shard_count", req.RepoShardCount),
		slog.Bool("listing_complete", req.Listing.Complete),
		slog.Int("listed_count", result.Counts.Listed),
		slog.Int("selectable_count", result.Counts.Selectable),
		slog.Int("archived_excluded_count", result.Counts.ArchivedExcluded),
		slog.Int("rule_excluded_count", result.Counts.RuleExcluded),
		slog.Int("known_scope_count", result.Counts.Known),
		slog.Int("newly_unlisted_count", result.Counts.NewlyUnlisted),
		slog.Int("not_listed_count", result.Counts.NotListed),
		slog.Int("relisted_count", result.Counts.Relisted),
		slog.Int64("evaluation_interval_seconds", int64(result.Batch.Interval/time.Second)),
		slog.String("outcome", string(result.Outcome)),
		slog.Any("not_listed_sample", result.NotListedSample),
	)
}

func (o Observer) warn(ctx context.Context, msg string, attrs ...slog.Attr) {
	if o.Logger == nil {
		return
	}
	o.Logger.LogAttrs(ctx, slog.LevelWarn, msg, append([]slog.Attr{log.CollectorKind(collectorKind)}, attrs...)...)
}
