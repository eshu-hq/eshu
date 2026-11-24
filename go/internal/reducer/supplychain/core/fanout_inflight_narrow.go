// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// States of the load-end in-flight read (set A), the in_flight_early_read log
// field and span attribute.
const (
	inFlightEarlyReadOK      = "ok"
	inFlightEarlyReadFailed  = "failed"
	inFlightEarlyReadSkipped = "skipped"
)

// inFlightEarlyReadFailedMessage is the warn line of a failed set A read.
const inFlightEarlyReadFailedMessage = "supply chain impact in-flight load-end read failed"

// inFlightClaimKey is one claim's identity: the scope and the exact
// last_attempt_at.
type inFlightClaimKey struct {
	scopeID   string
	claimedAt int64
}

func inFlightClaimKeyOf(claim InFlightClaim) inFlightClaimKey {
	return inFlightClaimKey{scopeID: claim.ScopeID, claimedAt: claim.ClaimedAt.UnixNano()}
}

// inFlightAtLoadEnd is set A of the #10972 D4b narrowing: the claims in flight
// when this pass's evidence load ended. Its zero value is "skipped", so a
// fan-out that was handed no set A flags every in-flight claim.
type inFlightAtLoadEnd struct {
	state    string
	claims   map[inFlightClaimKey]struct{}
	duration time.Duration
}

// stateLabel names the set A state for the log line and span.
func (a inFlightAtLoadEnd) stateLabel() string {
	if a.state == "" {
		return inFlightEarlyReadSkipped
	}
	return a.state
}

// flags reports whether a claim in set B must be flagged. Only a claim set A
// proves new (absent from a set A that was read) goes unflagged; a claim with
// no identity is always flagged.
func (a inFlightAtLoadEnd) flags(claim InFlightClaim) bool {
	if a.state != inFlightEarlyReadOK || claim.ClaimedAt.IsZero() {
		return true
	}
	_, held := a.claims[inFlightClaimKeyOf(claim)]
	return held
}

// readInFlightAtLoadEnd reads set A. The handler calls it right after
// loadSupplyChainImpactEvidence returns and before the writer, so every read
// that can add a fact to this pass's change set is behind it
// (HOLDER_FANOUT_INFLIGHT.md lists the reads after it). A one-hop rerun takes
// no read: it fans out only its suppression ids, and its fan-out flags every
// claim. A failed read never fails the pass; the fan-out then flags every
// claim in set B, as before D4b.
func (h SupplyChainImpactHandler) readInFlightAtLoadEnd(ctx context.Context, intent reducercontract.Intent, cause string) inFlightAtLoadEnd {
	if h.FanOut == nil || fanOutOneHop(intent) {
		return inFlightAtLoadEnd{state: inFlightEarlyReadSkipped}
	}
	started := time.Now()
	claims, err := h.FanOut.SnapshotInFlightNonMarkerClaims(ctx, intent.ScopeID)
	elapsed := time.Since(started)
	h.observeFanOutRead(ctx, telemetry.FanOutReadKindInFlightEarly, elapsed, err)
	if err != nil {
		h.countFanOutRead(ctx, cause, err, telemetry.FanOutReadSiteInFlightEarly)
		if h.Logger != nil {
			h.Logger.WarnContext(ctx, inFlightEarlyReadFailedMessage,
				slog.String("domain", string(reducercontract.DomainSupplyChainImpact)),
				slog.String("cause", cause),
				slog.String("scope_id", intent.ScopeID),
				slog.String("generation_id", intent.GenerationID),
				slog.String("intent_id", intent.IntentID),
				slog.String("outcome", readFailureOutcome(err)),
				slog.Float64("duration_seconds", elapsed.Seconds()),
				slog.String("error", err.Error()),
			)
		}
		return inFlightAtLoadEnd{state: inFlightEarlyReadFailed, duration: elapsed}
	}
	set := make(map[inFlightClaimKey]struct{}, len(claims))
	for _, claim := range claims {
		if !claim.ClaimedAt.IsZero() {
			set[inFlightClaimKeyOf(claim)] = struct{}{}
		}
	}
	return inFlightAtLoadEnd{state: inFlightEarlyReadOK, claims: set, duration: elapsed}
}

// inFlightNarrowing is what the in-flight arm did at fan-out, for the counter,
// the log line, and the span.
type inFlightNarrowing struct {
	earlyRead    string
	earlySeconds float64
	kept         int
	narrowed     int
}

func newInFlightNarrowing(early inFlightAtLoadEnd) inFlightNarrowing {
	return inFlightNarrowing{earlyRead: early.stateLabel(), earlySeconds: early.duration.Seconds()}
}

// keptOutcome is the counter outcome of a flagged claim: why it was kept.
func (n inFlightNarrowing) keptOutcome() string {
	switch n.earlyRead {
	case inFlightEarlyReadOK:
		return fanOutOutcomeInFlightKept
	case inFlightEarlyReadFailed:
		return fanOutOutcomeInFlightEarlyReadFailed
	default:
		return fanOutOutcomeInFlightEarlyReadSkipped
	}
}

// addInFlightHolders runs set B and adds the claims to flag to holders.
//
// The in-flight read comes FIRST, then the row read. A holder still claimed or
// running at the in-flight read is flagged. One that acknowledged before it has
// committed its rows (a pass commits before it acknowledges), so the row read
// that follows sees them. The other order leaves a gap: a holder with no rows
// at the first read that commits and acknowledges before the second is missed
// by both. A pass claimed after the source's evidence load ended (a claim in
// set B that set A does not hold) loads its own evidence later and sees every
// fact the source saw, so it needs no flag (#10972 D4b).
func (h SupplyChainImpactHandler) addInFlightHolders(
	ctx context.Context,
	intent reducercontract.Intent,
	cause string,
	early inFlightAtLoadEnd,
	holders map[string]struct{},
	span trace.Span,
) (inFlightNarrowing, error) {
	narrowing := newInFlightNarrowing(early)
	claims, err := h.FanOut.FindInFlightNonMarkerClaims(ctx, intent.ScopeID)
	if err != nil {
		h.countFanOutRead(ctx, cause, err, telemetry.FanOutReadSiteInFlight)
		span.RecordError(err)
		return narrowing, fanOutError{fmt.Errorf("find in-flight supply chain impact scopes: %w", err)}
	}
	for _, claim := range claims {
		if !early.flags(claim) {
			narrowing.narrowed++
			continue
		}
		narrowing.kept++
		holders[claim.ScopeID] = struct{}{}
	}
	span.SetAttributes(
		attribute.String("in_flight_early_read", narrowing.earlyRead),
		attribute.Int("in_flight_kept", narrowing.kept),
		attribute.Int("in_flight_narrowed", narrowing.narrowed),
	)
	return narrowing, nil
}

// emitInFlightNarrowing counts the in-flight arm's claims, one point per claim
// in set B.
func (h SupplyChainImpactHandler) emitInFlightNarrowing(ctx context.Context, cause string, n inFlightNarrowing) {
	h.countFanOut(ctx, cause, n.keptOutcome(), n.kept)
	h.countFanOut(ctx, cause, fanOutOutcomeInFlightNarrowed, n.narrowed)
}

// inFlightLogAttrs are the in-flight fields of the fan-out summary line.
func (n inFlightNarrowing) inFlightLogAttrs() []any {
	return []any{
		slog.String("in_flight_early_read", n.earlyRead),
		slog.Float64("in_flight_early_read_seconds", n.earlySeconds),
		slog.Int("in_flight_kept", n.kept),
		slog.Int("in_flight_narrowed", n.narrowed),
	}
}
