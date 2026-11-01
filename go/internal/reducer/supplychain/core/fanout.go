// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"go.opentelemetry.io/otel/attribute"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factwrite"
)

// The holder fan-out (#9695, item 1.5b) closes the convergence gap a source
// retraction leaves: a finding is derived by every scope that holds evidence
// for its CVE, and the winners table keeps one copy per canonical key, so a
// scope whose own facts did not change keeps a stale copy until something reruns
// it. After the writer commit, and before the queue acknowledgement, a pass that
// changed or retracted finding rows reopens the supply_chain_impact item of
// every scope that holds an active copy of those CVEs. The reopened pass
// re-derives its copy from current evidence, so the answer converges without a
// precedence rule in the winners query.
const (
	// fanOutCVEChunk bounds one holder-read statement to this many CVE ids.
	fanOutCVEChunk = 200
	// fanOutHolderCap bounds how many holders one claim reopens. The remainder
	// is carried on the committing item and drained by its next claims.
	fanOutHolderCap = 256

	// PayloadKeyFanOutOrigin is the work-item payload key a fan-out reopen
	// stamps. A pass that finds it is one hop from the pass that changed the
	// write and does not fan out again; it consumes the stamp.
	PayloadKeyFanOutOrigin = "fanout_origin"
	// PayloadKeyFanOutRemaining is the work-item payload key holding the sorted
	// holder scope ids a claim did not reach.
	PayloadKeyFanOutRemaining = "fanout_remaining"
	// PayloadKeyFanOutOwed is the work-item payload key the writer sets, inside
	// its own transaction, when a pass's commit changed finding rows. It says the
	// commit happened and the fan-out it owes has not finished, so a retry knows
	// whether an earlier attempt committed. The fan-out clears it when the last
	// holder is reopened.
	PayloadKeyFanOutOwed = "fanout_owed"
)

// FanOutOutcome is what reopening one holder did.
type FanOutOutcome string

// The closed set of per-holder reopen outcomes.
const (
	// FanOutReopened means a succeeded item went back to pending.
	FanOutReopened FanOutOutcome = "reopened"
	// FanOutReplayFlagged means a claimed or running item was marked so its
	// success acknowledgement returns it to pending once.
	FanOutReplayFlagged FanOutOutcome = "replay_flagged"
	// FanOutPendingNoop means the item was already queued or already flagged.
	FanOutPendingNoop FanOutOutcome = "pending_noop"
	// FanOutEnqueued means the holder's active generation had no queue item and
	// one was created.
	FanOutEnqueued FanOutOutcome = "enqueued"
)

// Pass-level counter outcomes that are not a per-holder result.
const (
	fanOutOutcomeCrashRecovery = "crash_recovery"
	fanOutOutcomeDeferred      = "deferred"
	fanOutOutcomeReadError     = "read_error"
	fanOutOutcomeReadTimeout   = "read_timeout"
	fanOutOutcomeReopenError   = "reopen_error"
	fanOutOutcomeStateError    = "state_error"
)

// ErrFanOutReadTimeout marks a holder read that exceeded its bounded statement
// timeout. The pass fails with it so the queue retries; the counter reports it
// as read_timeout rather than read_error.
var ErrFanOutReadTimeout = errors.New("supply chain impact holder read timed out")

// SupplyChainImpactFanOutFailureClass classifies a pass that failed in its holder
// fan-out. It is a counting class on purpose: the queue retries the pass up to
// its attempt budget and then dead-letters it, which is loud and replayable,
// whereas a non-counting class would retry a persistent failure forever.
const SupplyChainImpactFanOutFailureClass = "supply_chain_impact_fanout_failed"

// RetryableFanOutError marks err retryable and self-classified like the
// handler's own fan-out failures. The store returns it for a lost lease inside the
// writer's transaction, where a plain error would reach the queue as terminal.
func RetryableFanOutError(err error) error { return fanOutError{err} }

// fanOutError marks a fan-out failure as retryable and self-classified. The
// queue dead-letters any error that does not report Retryable() == true, so
// without this a single transient holder-read timeout, after the writer already
// committed, would dead-letter the pass instead of retrying it.
type fanOutError struct{ err error }

func (e fanOutError) Error() string { return e.err.Error() }
func (e fanOutError) Unwrap() error { return e.err }

// Retryable reports that the queue should retry the whole pass.
func (fanOutError) Retryable() bool { return true }

// FailureClass names the durable failure class for the retrying row.
func (fanOutError) FailureClass() string { return SupplyChainImpactFanOutFailureClass }

// FanOutOrigin names the pass that caused a reopen.
type FanOutOrigin struct {
	SourceScopeID      string
	SourceGenerationID string
	SourceIntentID     string
}

// FanOutOwedRecorder records, inside the writer's transaction, that the commit
// owes a fan-out. Recording it in the same transaction as the rows makes "an
// attempt committed" and "the rows are there" one fact: a crash cannot leave one
// without the other. A lost lease fails the record, and the writer rolls back.
type FanOutOwedRecorder interface {
	RecordFanOutOwed(ctx context.Context, exec factwrite.Execer) error
}

// FanOutState is the one lease-fenced update a pass makes to its own queue
// item: rewrite (or clear) the list of holders it did not reach, and consume the
// origin stamp it saw. The list is touched only when ReplaceRemaining is set, so
// an empty Remaining never means "clear" by accident; with ReplaceRemaining set
// an empty Remaining clears the list, and a non-empty one also marks the item so
// its acknowledgement returns it to pending and the next claim drains the list.
// The stamp is consumed only while the stored stamp still equals ExpectedOrigin,
// so a newer stamp written since the claim survives.
type FanOutState struct {
	Remaining        []string
	ReplaceRemaining bool
	ConsumeOrigin    bool
	ExpectedOrigin   map[string]any
	// ClearOwed removes the owed marker: every holder this commit owes has been
	// reopened, or the owed work moved to the continuation list.
	ClearOwed bool
}

// SupplyChainImpactFanOut is the queue-side port of the holder fan-out. The
// Postgres implementation lives in storage/postgres; the handler owns the
// decision of whether and whom to fan out to.
type SupplyChainImpactFanOut interface {
	// FindHolderScopes returns the scopes, other than ownScopeID, that hold an
	// active finding row for any of the CVE ids. It is one bounded statement per
	// call and fails with ErrFanOutReadTimeout when it exceeds its timeout.
	FindHolderScopes(ctx context.Context, ownScopeID string, cveIDs []string) ([]string, error)
	// ReopenHolder reopens the holder's supply_chain_impact item at the scope's
	// current active generation, stamping origin, or enqueues one when that
	// generation has none.
	ReopenHolder(ctx context.Context, holderScopeID string, origin FanOutOrigin) (FanOutOutcome, error)
	// FindInFlightScopes returns the scopes, other than ownScopeID, whose
	// supply_chain_impact item at the scope's active generation is claimed or
	// running. Such a pass may have loaded its evidence before the asking pass's
	// source facts were visible, and a scope whose new generation has no finding
	// rows yet is invisible to FindHolderScopes. Same bounded timeout and error
	// contract as FindHolderScopes.
	FindInFlightScopes(ctx context.Context, ownScopeID string) ([]string, error)
	// OwedRecorder returns the recorder the writer runs inside its transaction for
	// the calling pass's own item.
	OwedRecorder(intent reducercontract.Intent) FanOutOwedRecorder
	// UpdateFanOutState applies state to the calling pass's own item, fenced on
	// its lease.
	UpdateFanOutState(ctx context.Context, intent reducercontract.Intent, state FanOutState) error
}

// fanOutResult is what one pass's fan-out did, for sub-signals and the log.
type fanOutResult struct {
	holders       int
	reopened      int
	replayFlagged int
	pendingNoop   int
	enqueued      int
	// deferred is the number of holders still owed after this claim, a gauge
	// the sub-signal reports. newlyDeferred is the part that was not already
	// owed when the claim started, which is what the counter adds, so draining
	// one backlog across several claims counts each holder once.
	deferred      int
	newlyDeferred int
	crashRecovery bool
}

// fanOutHolders runs the holder fan-out for one committed pass. It returns an
// error for a failed holder read or reopen, so the pass fails before its
// acknowledgement and the queue retries it: a holder left unreopened would be a
// silently stale copy. A handler with no port wired does nothing.
func (h SupplyChainImpactHandler) fanOutHolders(
	ctx context.Context,
	intent reducercontract.Intent,
	write SupplyChainImpactWriteResult,
) (fanOutResult, error) {
	if h.FanOut == nil {
		return fanOutResult{}, nil
	}
	stamped := fanOutOriginStamped(intent)
	owed := fanOutOwed(intent)
	oneHop := fanOutOneHop(intent)
	remaining := fanOutRemaining(intent)
	var cveIDs []string
	recoveryOnly := false
	if !oneHop {
		// A continuation drain does not add the pass's own changes: they were
		// fanned out by the claim that wrote them.
		cveIDs = write.ChangedCVEIDs
		if owed && len(remaining) == 0 && len(write.RecoveryCVEIDs) > 0 {
			// An earlier attempt committed and died before it finished the
			// fan-out, so this rerun's own rewrite shows no change. Fan out for
			// every CVE the scope names.
			recoveryOnly = len(write.ChangedCVEIDs) == 0
			cveIDs = unionSortedStrings(cveIDs, write.RecoveryCVEIDs)
		}
	}
	state := FanOutState{ConsumeOrigin: stamped}
	if stamped {
		state.ExpectedOrigin = fanOutOriginOf(intent)
	}
	if len(cveIDs) == 0 && len(remaining) == 0 {
		if stamped || owed {
			// Nothing is owed after all; consume the stamp so a later reopen by
			// another path is judged fresh, and drop a marker with nothing behind it.
			state.ClearOwed = owed
			if err := h.FanOut.UpdateFanOutState(ctx, intent, state); err != nil {
				return fanOutResult{}, fanOutError{fmt.Errorf("clear supply chain impact fan-out state: %w", err)}
			}
		}
		return fanOutResult{}, nil
	}

	ctx, span := h.startFanOutSpan(ctx, intent)
	defer span.End()
	holders := make(map[string]struct{}, len(remaining))
	for _, id := range remaining {
		holders[id] = struct{}{}
	}
	// The in-flight read comes FIRST, then the row read. A holder still claimed or
	// running at the in-flight read is flagged. One that acknowledged before it has
	// committed its rows (a pass commits before it acknowledges), so the row read
	// that follows sees them. The other order leaves a gap: a holder with no rows
	// at the first read that commits and acknowledges before the second is missed
	// by both. A pass claimed after the in-flight read loads its evidence after the
	// source's facts became visible, so it needs no flag.
	if len(cveIDs) > 0 {
		inFlight, err := h.FanOut.FindInFlightScopes(ctx, intent.ScopeID)
		if err != nil {
			h.countFanOut(ctx, readFailureOutcome(err), 1)
			span.RecordError(err)
			return fanOutResult{}, fanOutError{fmt.Errorf("find in-flight supply chain impact scopes: %w", err)}
		}
		for _, id := range inFlight {
			holders[id] = struct{}{}
		}
	}
	for _, chunk := range chunkStrings(cveIDs, fanOutCVEChunk) {
		found, err := h.FanOut.FindHolderScopes(ctx, intent.ScopeID, chunk)
		if err != nil {
			h.countFanOut(ctx, readFailureOutcome(err), 1)
			span.RecordError(err)
			return fanOutResult{}, fanOutError{fmt.Errorf("find supply chain impact holders: %w", err)}
		}
		for _, id := range found {
			holders[id] = struct{}{}
		}
	}
	delete(holders, intent.ScopeID)
	ordered := make([]string, 0, len(holders))
	for id := range holders {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)

	batch, rest := nextFanOutChunk(ordered, fanOutHolderCap)
	result := fanOutResult{holders: len(ordered), deferred: len(rest), crashRecovery: recoveryOnly}
	if len(remaining) == 0 {
		result.newlyDeferred = len(rest)
	}
	origin := FanOutOrigin{
		SourceScopeID:      intent.ScopeID,
		SourceGenerationID: intent.GenerationID,
		SourceIntentID:     intent.IntentID,
	}
	for _, holder := range batch {
		outcome, err := h.FanOut.ReopenHolder(ctx, holder, origin)
		if err != nil {
			h.countFanOut(ctx, fanOutOutcomeReopenError, 1)
			span.RecordError(err)
			return result, fanOutError{fmt.Errorf("reopen supply chain impact holder %q: %w", holder, err)}
		}
		result.record(outcome)
		h.logFanOutHolder(ctx, intent, holder, outcome)
	}
	if len(rest) > 0 || len(remaining) > 0 {
		state.ReplaceRemaining = true
		state.Remaining = rest
	}
	// The marker stays while holders are still owed: the continuation list carries
	// the debt across claims and the last drain clears both.
	// This attempt's own commit set the marker inside its transaction, after the
	// claim snapshot the intent carries, so a changed commit clears it too.
	markerSet := !oneHop && len(write.ChangedCVEIDs) > 0
	state.ClearOwed = (owed || markerSet) && len(rest) == 0
	if state.ReplaceRemaining || state.ConsumeOrigin || state.ClearOwed {
		if err := h.FanOut.UpdateFanOutState(ctx, intent, state); err != nil {
			h.countFanOut(ctx, fanOutOutcomeStateError, 1)
			span.RecordError(err)
			return result, fanOutError{fmt.Errorf("record supply chain impact fan-out state: %w", err)}
		}
	}
	h.emitFanOut(ctx, intent, result)
	span.SetAttributes(
		attribute.Int("holders", result.holders),
		attribute.Int("reopened", result.reopened),
		attribute.Int("replay_flagged", result.replayFlagged),
		attribute.Int("deferred", result.deferred),
	)
	return result, nil
}

func (r *fanOutResult) record(outcome FanOutOutcome) {
	switch outcome {
	case FanOutReopened:
		r.reopened++
	case FanOutReplayFlagged:
		r.replayFlagged++
	case FanOutEnqueued:
		r.enqueued++
	default:
		r.pendingNoop++
	}
}

// fanOutOriginStamped reports whether the intent's item carries a fan-out
// origin stamp.
func fanOutOriginStamped(intent reducercontract.Intent) bool {
	value, ok := intent.Payload[PayloadKeyFanOutOrigin]
	return ok && value != nil
}

// fanOutOwed reports whether the intent's item carries the owed marker: a
// commit of this item changed rows and its fan-out has not finished.
func fanOutOwed(intent reducercontract.Intent) bool {
	owed, _ := intent.Payload[PayloadKeyFanOutOwed].(bool)
	return owed
}

// fanOutOneHop reports whether this pass is the single re-derivation a fan-out
// asked for, which re-derives its copy and stops. That is the attempt the replay
// created: a reopen of a succeeded item and the replay of a flagged one both
// leave attempt_count at 0, so the claim is attempt 1. A stamp that reaches a
// later attempt was written while a genuine pass was running and that pass then
// failed or crashed, so the retry is a genuine pass again. An owed marker means
// this item committed changes it has not finished fanning out, so it is genuine
// too, whatever stamp it carries.
func fanOutOneHop(intent reducercontract.Intent) bool {
	return fanOutOriginStamped(intent) && intent.AttemptCount <= 1 && !fanOutOwed(intent)
}

// fanOutOriginOf returns the stamp map the intent carries, or nil.
func fanOutOriginOf(intent reducercontract.Intent) map[string]any {
	stamp, _ := intent.Payload[PayloadKeyFanOutOrigin].(map[string]any)
	return stamp
}

// fanOutRemaining returns the continuation list stored on the intent's item,
// sorted and de-duplicated. The payload round-trips through JSON, so the list
// arrives as []any.
func fanOutRemaining(intent reducercontract.Intent) []string {
	var raw []string
	switch list := intent.Payload[PayloadKeyFanOutRemaining].(type) {
	case []string:
		raw = list
	case []any:
		for _, item := range list {
			if id, ok := item.(string); ok && id != "" {
				raw = append(raw, id)
			}
		}
	}
	return unionSortedStrings(nil, raw)
}

// nextFanOutChunk splits a sorted holder list into the first limit holders and
// the rest.
func nextFanOutChunk(sorted []string, limit int) ([]string, []string) {
	if len(sorted) <= limit {
		return sorted, nil
	}
	return sorted[:limit], sorted[limit:]
}

// chunkStrings splits values into slices of at most size, keeping order.
func chunkStrings(values []string, size int) [][]string {
	var chunks [][]string
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		chunks = append(chunks, values[start:end])
	}
	return chunks
}

// unionSortedStrings returns the sorted, de-duplicated union of a and b with
// blanks dropped.
func unionSortedStrings(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, list := range [][]string{a, b} {
		for _, id := range list {
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// owedRecorder returns the recorder the writer runs inside its transaction, or
// nil when there is no port or this pass is the one-hop re-derivation, which fans
// out nothing and so owes nothing.
func (h SupplyChainImpactHandler) owedRecorder(intent reducercontract.Intent) FanOutOwedRecorder {
	if h.FanOut == nil || fanOutOneHop(intent) {
		return nil
	}
	return h.FanOut.OwedRecorder(intent)
}
