// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// DeltaUnionCompletenessCapability names the delta-union completeness check
// in the capability matrix; envelope errors carry it verbatim.
const DeltaUnionCompletenessCapability = "operator.delta_union_completeness.check"

// EnvelopeMIMEType selects the stable Eshu response envelope, mirroring the
// core query contract value so negotiation matches byte-for-byte.
const EnvelopeMIMEType = "application/eshu.envelope+json"

// BackendUnavailableRetryAfterSeconds is the Retry-After hint, in seconds,
// sent only with transient retryable 503 verdicts. A permanent 503 carries
// no hint. It mirrors the core reader replay window (2s).
const BackendUnavailableRetryAfterSeconds = 2

// SnapshotExportUnqualifiedReason is the closed details.reason value of the
// permanent 503 for an unqualified shared-snapshot reader.
const SnapshotExportUnqualifiedReason = "snapshot_export_unqualified"

// Graph-verdict sentinels. Destination adapters wrap their Bolt/PG failures
// with these (via fmt.Errorf %w) so DeltaUnionGraphErrorHandler can classify
// without importing core querycontract/db packages. Their texts are the only
// strings a client may see; backend text never reaches the body.
var (
	// ErrGraphUnavailable reports the graph backend could not serve a read.
	ErrGraphUnavailable = errors.New("graph temporarily unavailable; retry after graph health is restored")
	// ErrGraphReadDeadline reports the bounded graph-read budget expired.
	ErrGraphReadDeadline = errors.New("graph query exceeded its deadline")
	// ErrReaderRetryable is the fixed-text face of a PG reader that could not
	// serve a fenced read in time (stale replica or timed-out pool wait).
	// Permanent reader failures must never wrap it.
	ErrReaderRetryable = errors.New("database read temporarily unavailable; retry shortly")
	// ErrSnapshotExportRefused is the permanent face of an unqualified
	// shared-snapshot reader build; it never carries a retry hint.
	ErrSnapshotExportRefused = errors.New("shared-snapshot read refused: the PostgreSQL reader build is not qualified for snapshot export")
)

// ClassifyDeltaUnionGraphReadError maps a graph-read error onto
// ErrGraphReadDeadline when the read ran out of time, mirroring the core
// bounded-read classifier: already-classified verdicts and reader-retryable
// or snapshot verdicts pass through unchanged, a canceled context is not a
// deadline, and a nil error stays nil.
func ClassifyDeltaUnionGraphReadError(ctx context.Context, err error) error {
	if err == nil || errors.Is(err, ErrGraphReadDeadline) || errors.Is(err, ErrGraphUnavailable) ||
		errors.Is(err, ErrReaderRetryable) || errors.Is(err, ErrSnapshotExportRefused) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) || (ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)) {
		return fmt.Errorf("%w: %w", ErrGraphReadDeadline, err)
	}
	return err
}

// deltaUnionGraphVerdict is one mapped graph-read verdict.
type deltaUnionGraphVerdict struct {
	status    int
	code      string
	message   string
	details   map[string]any
	retryable bool
}

// mapDeltaUnionGraphVerdict classifies err in core order: snapshot refusal
// first (permanent), then graph-unavailable, reader-retryable, deadline.
func mapDeltaUnionGraphVerdict(err error) (deltaUnionGraphVerdict, bool) {
	switch {
	case err == nil:
		return deltaUnionGraphVerdict{}, false
	case errors.Is(err, ErrSnapshotExportRefused):
		return deltaUnionGraphVerdict{status: http.StatusServiceUnavailable, code: "backend_unavailable", message: ErrSnapshotExportRefused.Error(), details: map[string]any{"reason": SnapshotExportUnqualifiedReason}}, true
	case errors.Is(err, ErrGraphUnavailable):
		return deltaUnionGraphVerdict{status: http.StatusServiceUnavailable, code: "backend_unavailable", message: ErrGraphUnavailable.Error(), retryable: true}, true
	case errors.Is(err, ErrReaderRetryable):
		return deltaUnionGraphVerdict{status: http.StatusServiceUnavailable, code: "backend_unavailable", message: ErrReaderRetryable.Error(), details: map[string]any{"retry_after_seconds": BackendUnavailableRetryAfterSeconds}, retryable: true}, true
	case errors.Is(err, ErrGraphReadDeadline):
		return deltaUnionGraphVerdict{status: http.StatusGatewayTimeout, code: "backend_timeout", message: ErrGraphReadDeadline.Error()}, true
	default:
		return deltaUnionGraphVerdict{}, false
	}
}

// graphVerdictError mirrors the core ErrorEnvelope field order so negotiated
// bytes match the core route.
type graphVerdictError struct {
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	Capability string         `json:"capability,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

// graphVerdictEnvelope mirrors the core ResponseEnvelope shape.
type graphVerdictEnvelope struct {
	Data  any                `json:"data"`
	Truth any                `json:"truth"`
	Error *graphVerdictError `json:"error"`
}

// acceptsEnvelope reports whether the request negotiated the Eshu envelope.
func acceptsEnvelope(r *http.Request) bool {
	return r != nil && strings.Contains(r.Header.Get("Accept"), EnvelopeMIMEType)
}

// DeltaUnionGraphErrorHandler is the non-nil StoreErrorHandler a host passes
// to ServeDeltaUnionCompleteness / MountDeltaUnionCompleteness to reproduce
// the core graph-verdict shapes (#11697): retryable 503 with Retry-After,
// 504 on deadline, permanent 503 without Retry-After, fixed verdict text
// only. Without envelope negotiation the body is exactly
// {"status":"error","detail":<fixed text>} (the core route shape, not the
// generic {error,detail}); with EnvelopeMIMEType it is the full envelope
// naming DeltaUnionCompletenessCapability. Unclaimed errors return false
// untouched for Serve's own 499/cap/fixed-500 mapping.
func DeltaUnionGraphErrorHandler(w http.ResponseWriter, r *http.Request, err error) bool {
	verdict, ok := mapDeltaUnionGraphVerdict(err)
	if !ok {
		return false
	}
	if verdict.retryable && verdict.status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", strconv.Itoa(BackendUnavailableRetryAfterSeconds))
	}
	if acceptsEnvelope(r) {
		writeJSON(w, verdict.status, graphVerdictEnvelope{
			Error: &graphVerdictError{
				Code:       verdict.code,
				Message:    verdict.message,
				Capability: DeltaUnionCompletenessCapability,
				Details:    verdict.details,
			},
		})
		return true
	}
	writeJSON(w, verdict.status, map[string]any{"status": "error", "detail": verdict.message})
	return true
}
