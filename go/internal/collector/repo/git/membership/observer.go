// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package membership

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
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
	// FailureClassExpiredSweep means deleting expired observation rows
	// failed. The evaluation outcome stands.
	FailureClassExpiredSweep = "expired_sweep"
	// FailureClassSelectorCycleUpsert means writing the selector's
	// per-cycle liveness marker (#7840) failed. The evaluation outcome
	// stands; only a marker stale for a full window plus grace lets the
	// sweep reclaim the selector's not_listed rows.
	FailureClassSelectorCycleUpsert = "selector_cycle_upsert"
	// FailureClassClassificationUpsert means writing upstream probe
	// classifications failed. The evaluation outcome stands and the next
	// cycle re-probes: every unclassified Confirmed not_listed row is a
	// target again.
	FailureClassClassificationUpsert = "classification_upsert"
)

// SweepCounts are the rows one DeleteExpiredObservations call deleted:
// Deleted in total, of which NotListedReclaimed were abandoned selectors'
// not_listed rows (#7840).
type SweepCounts struct {
	Deleted            int64
	NotListedReclaimed int64
}

// Store persists selection observations. KnownScopes and Observations are
// plain reads that take no row locks; UpsertObservations writes one batch in
// one statement and only advances rows older than the batch;
// DeleteExpiredObservations deletes long-expired rows in bounded batches;
// UpsertSelectorCycle advances a selector's per-cycle liveness marker.
type Store interface {
	// KnownScopes returns the git default-branch repository scopes in
	// partition: first-segment owner match, or path-prefix match for a
	// nested group partition, compared case-insensitively. A non-empty
	// host also requires the scope's stored remote_url host to equal it; a
	// scope without a remote then never matches.
	KnownScopes(ctx context.Context, partition Partition) ([]KnownScope, error)
	// Observations returns every stored observation for selectorID.
	Observations(ctx context.Context, selectorID string) ([]Observation, error)
	// UpsertObservations writes batch.
	UpsertObservations(ctx context.Context, batch Batch) error
	// DeleteExpiredObservations deletes rows of every selector whose
	// evaluated_at plus their own liveness window plus grace is before now,
	// in bounded batches, and returns the rows deleted, including those
	// deleted before an error. It never deletes a row inside its window or
	// the grace after it. A not_listed row, which Evaluate's mass-miss
	// guard reads, is deleted only when its selector's cycle marker is
	// missing or older than the row's window plus grace (#7840): a
	// selector that still cycles, even one whose guard trips every cycle,
	// keeps its guard history.
	DeleteExpiredObservations(ctx context.Context, now time.Time, grace time.Duration) (SweepCounts, error)
	// UpsertSelectorCycle records that selector ran a cycle at cycleAt with
	// liveness window window (#7840). The marker only advances: a replay or
	// a lagging replica at an older cycle time changes nothing. Every cycle
	// writes it, including a guard-tripped or listing-truncated one, so the
	// sweep can tell a live selector from an abandoned one.
	UpsertSelectorCycle(ctx context.Context, selector Selector, cycleAt time.Time, window time.Duration) error
	// UpsertClassifications writes probe verdicts for the selector's
	// Confirmed not_listed rows: one row per probed scope with its upstream
	// classification, successor slug, and probe time. Rows whose probe was
	// inconclusive never reach the store; their prior verdict stands. An
	// empty batch writes nothing.
	UpsertClassifications(ctx context.Context, selectorID string, rows []ClassificationRow, classifiedAt time.Time) error
}

// Request is one cycle's evaluation request from the git collector.
// LivenessWindow is the configured ESHU_REPO_SELECTION_LIVENESS_WINDOW; zero
// means DefaultLivenessWindow. SweepExpired asks Observe to run the
// expired-row sweep after this evaluation; the collector sets it on exactly
// one request in a cycle that issues any, so a cycle sweeps at most once
// whatever its number of explicit owners.
type Request struct {
	Selector       Selector
	SourceMode     string
	RepoShardCount int
	RepoLimit      int
	Now            time.Time
	LivenessWindow time.Duration
	Listing        Listing
	SweepExpired   bool
	// Probe answers one Confirmed not_listed scope's upstream state. Nil
	// disables probing; the git collector sets it for github_org selectors
	// only, bound to the listing credential, so gitlab_group and explicit
	// rows are never probed.
	Probe ProbeFunc
}

// Observer evaluates one selector per cycle and records the outcome. It never
// returns an error: a store failure is logged, counted as store_error, and the
// collector cycle continues with today's behavior.
type Observer struct {
	Store       Store
	Instruments *telemetry.Instruments
	Logger      *slog.Logger
}

// Observe evaluates req, writes the batch when the evaluation passes every
// rail, and emits the evaluation counter, the scope gauge (evaluated
// github_org cycles only), and the git_repository_selection_* logs. When the
// gap since the selector's previous evaluation exceeds the liveness window it
// also logs git_repository_selection_liveness_lapsed: the selector's rows had
// expired and read as unknown until this evaluation.
//
// Every cycle with a wired store also advances the selector's liveness
// marker (#7840), whatever the outcome: a guard-tripped cycle writes no
// observation rows, so the marker is the only proof its selector is still
// cycling. A marker failure is logged as the selector_cycle_upsert failure
// class and leaves the outcome unchanged. The marker is written before the
// sweep, so this cycle's own sweep can never reclaim this selector's rows.
//
// When req.SweepExpired is set, a store is wired, and no store call failed
// (an evaluated, guard-tripped, or listing-truncated outcome), it then sweeps
// rows of every selector that expired more than ExpiredObservationGrace ago.
// A not_listed row is swept only for a selector whose marker is missing or
// older than the row's window plus the grace. A truncated listing still
// sweeps: the predicate is time-based and independent of the listing, so a
// repo limit held below the org size cannot stop the drain. A sweep failure
// is logged as the expired_sweep failure class and leaves the outcome
// unchanged.
func (o Observer) Observe(ctx context.Context, req Request) Result {
	result, failureClass, err := o.evaluate(ctx, req)
	if failureClass != "" {
		result.Outcome = OutcomeStoreError
		o.storeFailed(ctx, req, failureClass, err)
	}
	if o.Store != nil {
		if err := o.Store.UpsertSelectorCycle(ctx, req.Selector, req.Now, livenessWindow(req.LivenessWindow)); err != nil {
			o.storeFailed(ctx, req, FailureClassSelectorCycleUpsert, err)
		}
	}
	if req.SweepExpired && o.Store != nil && sweepsAfter(result.Outcome) {
		var counts SweepCounts
		counts, err = o.Store.DeleteExpiredObservations(ctx, req.Now, ExpiredObservationGrace)
		result.ExpiredDeleted, result.NotListedReclaimed = counts.Deleted, counts.NotListedReclaimed
		if err != nil {
			o.storeFailed(ctx, req, FailureClassExpiredSweep, err)
		}
	}
	o.record(ctx, req.Selector.Kind, result)
	gap := evaluationGap(req.Now, result.PreviousEvaluatedAt)
	window := livenessWindow(req.LivenessWindow)
	if !result.PreviousEvaluatedAt.IsZero() && gap > window {
		o.warn(ctx, "git_repository_selection_liveness_lapsed",
			slog.String("selector_id", req.Selector.ID),
			slog.String("selector_kind", req.Selector.Kind),
			slog.Int64("evaluation_gap_seconds", int64(gap/time.Second)),
			slog.Int64("liveness_window_seconds", int64(window/time.Second)),
		)
	}
	o.logEvaluated(ctx, req, result, gap, window)
	return result
}

// sweepsAfter reports whether a request with this outcome may run the
// expired-row sweep: every outcome except a store failure.
func sweepsAfter(outcome Outcome) bool {
	switch outcome {
	case OutcomeEvaluated, OutcomeGuardTripped, OutcomeListingTruncated:
		return true
	default:
		return false
	}
}

// evaluationGap is the time since the selector's previous evaluation, zero on
// its first evaluation.
func evaluationGap(now, previous time.Time) time.Duration {
	if previous.IsZero() {
		return 0
	}
	return now.UTC().Truncate(time.Microsecond).Sub(previous)
}

func (o Observer) evaluate(ctx context.Context, req Request) (Result, string, error) {
	in := Input{Selector: req.Selector, Now: req.Now, LivenessWindow: req.LivenessWindow, Listing: req.Listing}
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
	if req.Selector.Kind == KindGitLabGroup && strings.TrimSpace(req.Selector.knownHost()) == "" {
		return listingOnly, FailureClassKnownScopesRead, errors.New("gitlab_group selector has no host")
	}
	known, err := o.Store.KnownScopes(ctx, req.Selector.Partition())
	if err != nil {
		return listingOnly, FailureClassKnownScopesRead, fmt.Errorf("read known repository scopes for owner %q: %w", req.Selector.Owner, err)
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
		if req.Selector.Kind == KindGitHubOrg && req.Probe != nil {
			o.classifyProbed(ctx, req, known, prior, &result)
		}
	case OutcomeListingTruncated, OutcomeStoreError:
		// Unreachable: a truncated listing returned above, and Evaluate never
		// reports a store error.
	}
	return result, "", nil
}

// classifyProbed probes the selector's Confirmed not_listed rows and writes
// the verdicts (#8454). Targets come from the post-write projection joined
// with the prior classifications, so a scope this very cycle confirmed
// probes at once instead of waiting one more. An inconclusive probe keeps
// the prior verdict and writes nothing; a probe error counts as kept_prior,
// never as a failed cycle. A failed classification write is the
// classification_upsert WARN class and leaves the evaluation outcome
// unchanged, with no gauge sample: the gauge never reports what the store
// does not hold.
func (o Observer) classifyProbed(ctx context.Context, req Request, known []KnownScope, prior []Observation, result *Result) {
	ctx, cancel := context.WithTimeout(ctx, ProbePassTimeout)
	defer cancel()
	result.ProbeRan = true
	result.ProbeOutcomes = make(map[string]int)
	priorByScope := make(map[string]Observation, len(prior))
	for _, observation := range prior {
		priorByScope[observation.ScopeID] = observation
	}
	knownByScope := make(map[string]KnownScope, len(known))
	for _, scope := range known {
		knownByScope[scope.ScopeID] = scope
	}
	merged := make([]Observation, 0, len(result.Projected))
	for _, projected := range result.Projected {
		if previous, ok := priorByScope[projected.ScopeID]; ok {
			projected.Classification = previous.Classification
			projected.SuccessorSlug = previous.SuccessorSlug
			projected.ClassifiedAt = previous.ClassifiedAt
		}
		merged = append(merged, projected)
	}
	targets := SelectProbeTargets(merged, knownByScope, req.Selector.Owner, req.Now)
	updated := make(map[string]ClassificationRow, len(targets))
	for _, target := range targets {
		answer, err := req.Probe(ctx, target)
		if err != nil {
			result.ProbeOutcomes[telemetry.RepositorySelectionProbeOutcomeKeptPrior]++
			continue
		}
		classification, successor, update := Classify(target, answer)
		if !update {
			result.ProbeOutcomes[telemetry.RepositorySelectionProbeOutcomeKeptPrior]++
			continue
		}
		result.ProbeOutcomes[string(classification)]++
		updated[target.ScopeID] = ClassificationRow{ScopeID: target.ScopeID, Classification: classification, SuccessorSlug: successor}
	}
	rows := make([]ClassificationRow, 0, len(updated))
	for _, row := range updated {
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b ClassificationRow) int { return strings.Compare(a.ScopeID, b.ScopeID) })
	if len(rows) > 0 {
		if err := o.Store.UpsertClassifications(ctx, req.Selector.ID, rows, result.Batch.EvaluatedAt); err != nil {
			o.storeFailed(ctx, req, FailureClassClassificationUpsert, fmt.Errorf("upsert %d repository selection classifications: %w", len(rows), err))
			return
		}
	}
	result.Classifications = postCycleClassifications(merged, updated)
}

// postCycleClassifications counts merged's Confirmed not_listed rows by
// classification after applying updated verdicts, reporting an empty
// classification as unclassified.
func postCycleClassifications(merged []Observation, updated map[string]ClassificationRow) map[string]int {
	counts := make(map[string]int)
	for _, observation := range merged {
		if observation.State != StateNotListed || !Confirmed(observation) {
			continue
		}
		classification := string(observation.Classification)
		if row, ok := updated[observation.ScopeID]; ok {
			classification = string(row.Classification)
		}
		if classification == "" {
			classification = telemetry.RepositorySelectionClassificationUnclassified
		}
		counts[classification]++
	}
	return counts
}

func (o Observer) storeFailed(ctx context.Context, req Request, failureClass string, err error) {
	o.warn(ctx, "git_repository_selection_store_failed",
		slog.String("selector_id", req.Selector.ID),
		slog.String("selector_kind", req.Selector.Kind),
		log.FailureClass(failureClass),
		log.Err(err),
	)
}

func (o Observer) record(ctx context.Context, selectorKind string, result Result) {
	if o.Instruments == nil {
		return
	}
	if result.ExpiredDeleted > 0 && o.Instruments.RepositorySelectionObservationsDeleted != nil {
		o.Instruments.RepositorySelectionObservationsDeleted.Add(ctx, result.ExpiredDeleted, metric.WithAttributes(
			telemetry.AttrCollectorKind(collectorKind),
		))
	}
	if result.NotListedReclaimed > 0 && o.Instruments.RepositorySelectionNotListedReclaimed != nil {
		o.Instruments.RepositorySelectionNotListedReclaimed.Add(ctx, result.NotListedReclaimed, metric.WithAttributes(
			telemetry.AttrCollectorKind(collectorKind),
		))
	}
	if o.Instruments.RepositorySelectionEvaluations != nil {
		o.Instruments.RepositorySelectionEvaluations.Add(ctx, 1, metric.WithAttributes(
			telemetry.AttrCollectorKind(collectorKind),
			telemetry.AttrSelectorKind(selectorKind),
			telemetry.AttrOutcome(string(result.Outcome)),
		))
	}
	// The gauge describes one selector's known scopes; an explicit selector
	// writes only selected rows for its own list, so it would overwrite the
	// selector's sample with a partial count. One collector process runs one
	// source mode, so no two kinds share a series.
	if result.Outcome != OutcomeEvaluated || (selectorKind != KindGitHubOrg && selectorKind != KindGitLabGroup) || o.Instruments.RepositorySelectionScopes == nil {
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
	if result.ProbeRan {
		if o.Instruments.RepositorySelectionProbe != nil {
			for outcome, count := range result.ProbeOutcomes {
				o.Instruments.RepositorySelectionProbe.Add(ctx, int64(count), metric.WithAttributes(
					telemetry.AttrCollectorKind(collectorKind),
					telemetry.AttrOutcome(outcome),
				))
			}
		}
		if o.Instruments.RepositorySelectionClassifications != nil && result.Classifications != nil {
			for classification, count := range result.Classifications {
				o.Instruments.RepositorySelectionClassifications.Record(ctx, int64(count), metric.WithAttributes(
					telemetry.AttrCollectorKind(collectorKind),
					telemetry.AttrClassification(classification),
				))
			}
		}
	}
}

func (o Observer) logEvaluated(ctx context.Context, req Request, result Result, gap, window time.Duration) {
	if o.Logger == nil {
		return
	}
	probed := 0
	for _, count := range result.ProbeOutcomes {
		probed += count
	}
	o.Logger.InfoContext(ctx, "git_repository_selection_evaluated",
		log.CollectorKind(collectorKind),
		slog.String("selector_id", req.Selector.ID),
		slog.String("selector_kind", req.Selector.Kind),
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
		slog.Int64("evaluation_gap_seconds", int64(gap/time.Second)),
		slog.Int64("liveness_window_seconds", int64(window/time.Second)),
		slog.String("outcome", string(result.Outcome)),
		slog.Int64("expired_deleted_count", result.ExpiredDeleted),
		slog.Int64("not_listed_reclaimed_count", result.NotListedReclaimed),
		slog.Any("not_listed_sample", result.NotListedSample),
		slog.Int("probed_count", probed),
		slog.Any("probe_outcomes", result.ProbeOutcomes),
		slog.Any("classifications", result.Classifications),
	)
}

func (o Observer) warn(ctx context.Context, msg string, attrs ...slog.Attr) {
	if o.Logger == nil {
		return
	}
	o.Logger.LogAttrs(ctx, slog.LevelWarn, msg, append([]slog.Attr{log.CollectorKind(collectorKind)}, attrs...)...)
}
