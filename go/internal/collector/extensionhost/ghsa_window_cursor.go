// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/vulnsource"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ghsaCoveredThroughPayloadKey is the raw snapshot key the host reads the
// satellite's stated coverage from (eshu-hq/eshu#10037 S3a/S3c). It is the
// typed contract field (sdk/go/factschema SourceSnapshot.CoveredThrough),
// read off the raw payload here by the NVD verdict-key precedent: the
// host judges the exact bytes the satellite emitted, never a re-encode.
const ghsaCoveredThroughPayloadKey = "covered_through"

// ghsaCacheModePayloadKey is the raw snapshot key stating how the
// satellite's cache layer produced the snapshot. Anything but "refresh"
// is a replay, and a replay never advances the cursor.
const ghsaCacheModePayloadKey = "cache_mode"

// GHSAWindowCommitPolicy is the ClaimCommitPolicy for rolling GHSA
// window scopes (eshu-hq/eshu#10037 S3c). It reads covered_through from
// the raw source-snapshot payload, validates it against the claimed
// window bounds, and arms the per-stream cursor compare-and-advance with
// WriteOnce set. Every refusal fails the claim retryable and commits
// nothing, so the cursor cannot pass unstored facts and a bad window
// re-plans instead of wedging:
//
//   - no snapshot, or no usable covered_through: held;
//   - cache replay (cache_mode present and not refresh): held;
//   - covered_through at or before the window start: no_progress;
//   - covered_through past the window end: out of range;
//   - covered inside the window: prefix; at the end: advanced.
//
// It is safe for concurrent use: it holds only immutable configuration,
// and the cursor store's compare-and-advance orders concurrent commits.
type GHSAWindowCommitPolicy struct {
	collectorInstanceID string
	instruments         *telemetry.Instruments
	logger              *slog.Logger
	clock               func() time.Time
}

var _ ClaimCommitPolicy = (*GHSAWindowCommitPolicy)(nil)

// NewGHSAWindowCommitPolicy builds the policy for one collector instance.
// A blank instance id is a construction error: a cursor row keyed under
// an invented instance would strand the stream. Instruments and logger
// are optional; nil skips the signal.
func NewGHSAWindowCommitPolicy(collectorInstanceID string, instruments *telemetry.Instruments, logger *slog.Logger) (*GHSAWindowCommitPolicy, error) {
	instanceID := strings.TrimSpace(collectorInstanceID)
	if instanceID == "" {
		return nil, errors.New("ghsa window commit policy requires a collector instance id")
	}
	return &GHSAWindowCommitPolicy{
		collectorInstanceID: instanceID,
		instruments:         instruments,
		logger:              logger,
		clock:               time.Now,
	}, nil
}

// CommitPolicy implements ClaimCommitPolicy for GHSA window scopes.
// Every other scope is not handled and keeps its existing behavior.
func (p *GHSAWindowCommitPolicy) CommitPolicy(
	item workflow.WorkItem,
	observedAt time.Time,
	envelopes []facts.Envelope,
) (ClaimCommitDecision, error) {
	advisoryType, start, end, ok := vulnsource.ParseGHSAWindowScope(item.ScopeID)
	if !ok {
		return ClaimCommitDecision{}, nil
	}
	ctx := context.Background()
	refuse := func(class, outcome string, err error) (ClaimCommitDecision, error) {
		p.instruments.RecordGHSAWindowAdvance(ctx, advisoryType, outcome)
		if p.logger != nil {
			p.logger.Warn("ghsa window claim refused by commit policy, nothing committed",
				telemetry.LogKeyFailureClass, class,
				telemetry.MetricDimensionAdvisoryType, advisoryType,
				"work_item_id", strings.TrimSpace(item.WorkItemID),
				"generation_id", strings.TrimSpace(item.GenerationID),
				"error", err.Error(),
			)
		}
		return ClaimCommitDecision{Handled: true}, &CommitPolicyRefusal{Class: class, Err: err}
	}
	payload, found := ghsaWindowSnapshotPayload(envelopes)
	if !found {
		return refuse(FailureClassGHSAWindowHeld, telemetry.GHSAWindowOutcomeHeld,
			errors.New("ghsa window generation carries no source snapshot stating covered_through"))
	}
	if mode, present := payload[ghsaCacheModePayloadKey].(string); present && mode != "refresh" {
		return refuse(FailureClassGHSACacheReplay, telemetry.GHSAWindowOutcomeHeld,
			errors.New("ghsa window snapshot is a cache replay; a replay never advances the cursor"))
	}
	coveredRaw, present := payload[ghsaCoveredThroughPayloadKey].(string)
	if !present || strings.TrimSpace(coveredRaw) == "" {
		return refuse(FailureClassGHSAWindowHeld, telemetry.GHSAWindowOutcomeHeld,
			errors.New("ghsa window snapshot states no covered_through"))
	}
	covered, err := time.Parse(time.RFC3339, coveredRaw)
	if err != nil {
		return refuse(FailureClassGHSAWindowHeld, telemetry.GHSAWindowOutcomeHeld,
			errors.New("ghsa window covered_through is not RFC3339"))
	}
	covered = vulnsource.NormalizeGHSAWindowTime(covered)
	switch {
	case !covered.After(start):
		return refuse(FailureClassGHSAWindowNoProgress, telemetry.GHSAWindowOutcomeNoProgress,
			errors.New("ghsa window covered_through does not pass the window start"))
	case covered.After(end):
		return refuse(FailureClassGHSACoveredThroughOutOfRange, telemetry.GHSAWindowOutcomeHeld,
			errors.New("ghsa window covered_through is past the window end"))
	}
	outcome := telemetry.GHSAWindowOutcomePrefix
	if covered.Equal(end) {
		outcome = telemetry.GHSAWindowOutcomeAdvanced
	}
	if observedAt.IsZero() {
		observedAt = p.clock().UTC()
	}
	p.instruments.RecordGHSAWindowAdvance(ctx, advisoryType, outcome)
	if p.instruments != nil {
		p.instruments.GHSACursorLags.RecordCursorPosition(advisoryType, covered.UnixMicro())
	}
	return ClaimCommitDecision{
		Handled: true,
		StateWriter: vulnerabilitystore.GHSAWindowCursorWriter(
			p.collectorInstanceID, advisoryType, covered.UnixMicro(), observedAt.UTC()),
		WriteOnce: true,
	}, nil
}

// ghsaWindowSnapshotPayload returns the raw payload of the first source
// snapshot fact in the generation, or false when none is present.
func ghsaWindowSnapshotPayload(envelopes []facts.Envelope) (map[string]any, bool) {
	for _, envelope := range envelopes {
		if envelope.FactKind != nvdWindowSnapshotFactKind {
			continue
		}
		if len(envelope.Payload) == 0 {
			return nil, false
		}
		return envelope.Payload, true
	}
	return nil, false
}
