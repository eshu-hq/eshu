// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/vulnsource"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// nvdWindowTransportPayloadKey is the satellite-stamped transport that served
// one NVD rolling-window claim (eshu-hq/eshu#11287). The satellite stamps
// exactly "api" or "feed" into the source snapshot payload beside the window
// verdict keys; the host folds it onto the closed transport set in
// telemetry.nvd_window.go. An absent, blank, or non-string stamp reads as
// unknown, never as feed (fail-closed): unknown covers an unstamped
// satellite and any API walk that did not stamp, so a feed series always
// proves a satellite stamped feed.
//
// The satellite half lives in eshu-collector-vulnerability-intelligence: it
// must stamp this key on every served window (feed and API alike) and name
// the feed failure class on retryable feed failures. Until that pin lands,
// every window reads unknown and no feed series exists.
const nvdWindowTransportPayloadKey = "nvd_window_transport"

// nvdWindowTransport folds one satellite-stamped snapshot payload onto the
// closed transport set. Anything but an exact "api" or "feed" string reads
// as unknown, so satellite text never becomes a label and a missing stamp
// never mints a feed series.
func nvdWindowTransport(payload map[string]any) string {
	stamped, ok := payload[nvdWindowTransportPayloadKey].(string)
	if !ok {
		return telemetry.NVDTransportUnknown
	}
	return telemetry.NVDTransport(stamped)
}

// nvdWindowTransportFromSDKFacts reads the transport from the first source
// snapshot fact in one SDK result, or unknown when no snapshot fact carries
// a stamp. Failed satellite results keep their snapshot facts, so a
// feed-attempted failure can still read feed; a host failure before the
// satellite runs carries no result and reads unknown.
func nvdWindowTransportFromSDKFacts(facts []sdkcollector.Fact) string {
	for _, fact := range facts {
		if fact.Kind != nvdWindowSnapshotFactKind {
			continue
		}
		if len(fact.Payload) == 0 {
			return telemetry.NVDTransportUnknown
		}
		return nvdWindowTransport(fact.Payload)
	}
	return telemetry.NVDTransportUnknown
}

// nvdWindowPending is the host-side observation one completed NVD window
// claim stashes between NextClaimed and the commit hook: the transport that
// served it, whether the commit armed the cursor advance, the cursor and
// head the verdict stamped, and when the claim started for the duration
// histogram. The generation guards a commit hook from settling a newer
// attempt's stash, mirroring pendingClaimOutcomes.
type nvdWindowPending struct {
	transport    string
	advanced     bool
	cursor       int64
	head         int64
	attemptedAt  time.Time
	generationID string
}

// pendingNVDWindowClaims stashes completed NVD window observations between
// NextClaimed and the commit hook, keyed by work item id. It mirrors
// pendingClaimOutcomes: one mutex-guarded map behind a pointer shared by
// every copy of the Source, because several ClaimedService workers run
// against one Source and share only the map.
type pendingNVDWindowClaims struct {
	mu   sync.Mutex
	rows map[string]nvdWindowPending
}

func newPendingNVDWindowClaims() *pendingNVDWindowClaims {
	return &pendingNVDWindowClaims{rows: make(map[string]nvdWindowPending)}
}

func (p *pendingNVDWindowClaims) stash(item workflow.WorkItem, pending nvdWindowPending) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rows[item.WorkItemID] = pending
}

func (p *pendingNVDWindowClaims) take(item workflow.WorkItem) (nvdWindowPending, bool) {
	if p == nil {
		return nvdWindowPending{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	pending, ok := p.rows[item.WorkItemID]
	if !ok || pending.generationID != item.GenerationID {
		return nvdWindowPending{}, false
	}
	delete(p.rows, item.WorkItemID)
	return pending, true
}

func (p *pendingNVDWindowClaims) drop(item workflow.WorkItem) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.rows, item.WorkItemID)
}

// recordNVDWindowClaim counts one NVD window claim at its final outcome
// through the production host instruments (eshu-hq/eshu#11287). Transport and
// outcome ride the closed sets in telemetry.nvd_window.go, so satellite text
// never becomes a label. Nil instruments skip the signal.
//
// Claims and duration count for every counted outcome (committed, refused,
// failed_retryable, failed_terminal); cursor advances count for committed
// windows only, advanced when the commit armed the cursor compare-and-advance
// and held when the facts committed and the verdict held the cursor. A claim
// that fails before the satellite runs, or is rejected for an identity
// mismatch, is not counted: callers skip those before reaching here.
func (s *Source) recordNVDWindowClaim(ctx context.Context, transport, outcome string, durationSeconds float64, advanced bool) {
	inst := s.instruments
	if inst == nil {
		return
	}
	transport = telemetry.NVDTransport(transport)
	attrs := metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionTransport, transport),
		attribute.String(telemetry.MetricDimensionOutcome, outcome),
	)
	if inst.NVDWindowClaims != nil {
		inst.NVDWindowClaims.Add(ctx, 1, attrs)
	}
	if inst.NVDWindowClaimDuration != nil {
		inst.NVDWindowClaimDuration.Record(ctx, durationSeconds, attrs)
	}
	if outcome != telemetry.NVDWindowClaimCommitted {
		return
	}
	advance := telemetry.NVDWindowAdvanceHeld
	if advanced {
		advance = telemetry.NVDWindowAdvanceAdvanced
	}
	if inst.NVDWindowCursorAdvances != nil {
		inst.NVDWindowCursorAdvances.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionTransport, transport),
			attribute.String(telemetry.MetricDimensionOutcome, advance),
		))
	}
}

// stashNVDWindowForCommit records the host observation for one completed NVD
// window claim whose generation ClaimedService will commit or refuse. It is
// a no-op for non-window scopes and nil instruments.
func (s *Source) stashNVDWindowForCommit(item workflow.WorkItem, transport string, advanced bool, cursor, head int64, attemptedAt time.Time) {
	if !vulnsource.IsNVDWindowScope(item.ScopeID) || s.instruments == nil {
		return
	}
	s.pendingNVD.stash(item, nvdWindowPending{
		transport:    telemetry.NVDTransport(transport),
		advanced:     advanced,
		cursor:       cursor,
		head:         head,
		attemptedAt:  attemptedAt,
		generationID: item.GenerationID,
	})
}

// emitNVDWindowCommitted counts one committed NVD window claim from the
// stashed observation: claims, duration, and advance, plus the cursor-lag
// position when the commit advanced the cursor. Absent a stashed observation
// it does nothing.
func (s *Source) emitNVDWindowCommitted(ctx context.Context, item workflow.WorkItem) {
	pending, ok := s.pendingNVD.take(item)
	if !ok {
		return
	}
	duration := s.clock().UTC().Sub(pending.attemptedAt).Seconds()
	if duration < 0 {
		duration = 0
	}
	s.recordNVDWindowClaim(ctx, pending.transport, telemetry.NVDWindowClaimCommitted, duration, pending.advanced)
	if pending.advanced && pending.cursor > 0 && pending.head > 0 && s.instruments != nil && s.instruments.NVDCursorLags != nil {
		s.instruments.NVDCursorLags.RecordCursorPosition("nvd", pending.transport, s.collectorInstanceID, pending.cursor, pending.head)
	}
}

// emitNVDWindowRefused counts one refused NVD window commit from the stashed
// observation: claims and duration with outcome refused. Advances and lag
// stay untouched because nothing committed.
func (s *Source) emitNVDWindowRefused(ctx context.Context, item workflow.WorkItem) {
	pending, ok := s.pendingNVD.take(item)
	if !ok {
		return
	}
	duration := s.clock().UTC().Sub(pending.attemptedAt).Seconds()
	if duration < 0 {
		duration = 0
	}
	s.recordNVDWindowClaim(ctx, pending.transport, telemetry.NVDWindowClaimRefused, duration, false)
}

// observeNVDWindowClaim routes one decided NVD window claim to its stash or
// failure count (eshu-hq/eshu#11287). Completed claims stash their transport
// and advance decision for the commit hook, which emits committed or refused.
// Satellite-signaled failures emit failed_retryable or failed_terminal at
// once with the stamped transport, or unknown when the satellite stamped
// nothing. Host failures before the satellite runs carry an empty result and
// are never counted, as are identity mismatches, per the instrument
// contract. A completed claim whose attempt-row write fails (source_state
// record) emits failed_retryable with its stamped transport.
func (s *Source) observeNVDWindowClaim(ctx context.Context, item workflow.WorkItem, attemptedAt time.Time, collected collector.CollectedGeneration, result sdkcollector.Result, ok bool, err error) {
	if !vulnsource.IsNVDWindowScope(item.ScopeID) || s.instruments == nil {
		return
	}
	if ok && err == nil {
		transport := nvdWindowTransportFromSDKFacts(result.Facts)
		advanced := collected.StateWriter != nil
		var cursor, head int64
		for _, fact := range result.Facts {
			if fact.Kind != nvdWindowSnapshotFactKind {
				continue
			}
			if value, ok := payloadMicros(fact.Payload, nvdWindowAdvanceToPayloadKey); ok {
				cursor = value
			}
			if value, ok := payloadMicros(fact.Payload, nvdWindowHeadPayloadKey); ok {
				head = value
			}
			break
		}
		s.stashNVDWindowForCommit(item, transport, advanced, cursor, head, attemptedAt)
		return
	}
	if len(result.Facts) == 0 && result.Generation.ID == "" {
		return
	}
	var failure extensionFailure
	class := ""
	terminal := false
	if errors.As(err, &failure) {
		class = failure.class
		terminal = failure.terminal
		if class == FailureClassInvalidClaim || class == FailureClassIdentityMismatch {
			return
		}
		if class == FailureClassInvalidResult || class == FailureClassLaunchFailure || class == FailureClassStatusRecord {
			return
		}
	}
	transport := nvdWindowTransportFromSDKFacts(result.Facts)
	s.emitNVDWindowFailed(ctx, item, transport, class, terminal, attemptedAt)
}

// emitNVDWindowFailed counts one satellite-signaled NVD window failure with
// the transport the satellite stamped, or unknown when it stamped nothing.
// Host failures before the satellite runs carry no result and are never
// counted: callers check for a satellite result before reaching here.
func (s *Source) emitNVDWindowFailed(ctx context.Context, item workflow.WorkItem, transport, failureClass string, terminal bool, attemptedAt time.Time) {
	if !vulnsource.IsNVDWindowScope(item.ScopeID) {
		return
	}
	outcome := telemetry.NVDWindowClaimRetryable
	if terminal {
		outcome = telemetry.NVDWindowClaimTerminal
	}
	duration := s.clock().UTC().Sub(attemptedAt).Seconds()
	if duration < 0 {
		duration = 0
	}
	s.recordNVDWindowClaim(ctx, transport, outcome, duration, false)
	if s.instruments != nil && failureClass == telemetry.NVDFeedFailureSHA256Mismatch {
		if s.instruments.NVDFeedSHA256Mismatch != nil {
			s.instruments.NVDFeedSHA256Mismatch.Add(ctx, 1)
		}
	}
}
