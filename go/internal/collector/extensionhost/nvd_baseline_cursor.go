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

// Baseline satellite-stamped checkpoint verdict keys. The satellite stamps
// exactly these keys into the source snapshot payload beside
// coverage_descriptor, by the NVD window verdict-key precedent: the commit
// path reads advance-to, seed sha, completion, and should-advance. The
// host never arms the NVD window cursor writer for a baseline scope, so a
// baseline verdict never moves the window cursor.
const (
	nvdBaselineAdvanceToPayloadKey     = "nvd_baseline_advance_to_unix_us"
	nvdBaselineYearSHAPayloadKey       = "nvd_baseline_year_sha256"
	nvdBaselineYearCompletePayloadKey  = "nvd_baseline_year_complete"
	nvdBaselineShouldAdvancePayloadKey = "nvd_baseline_should_advance"
)

// FailureClassNVDBaselineWindowWriterArmed marks a baseline generation
// that would have armed the NVD window cursor writer (#10728). The claim
// fails retryable and nothing commits: the cursor CAS never sees a
// baseline candidate, and the next plan replays the same chunk.
const FailureClassNVDBaselineWindowWriterArmed = "nvd_baseline_window_writer_armed"

// NVDBaselineCommitPolicy is the ClaimCommitPolicy for NVD baseline chunk
// scopes (eshu-hq/eshu#10728). It reads the satellite-stamped checkpoint
// verdict from the raw source-snapshot payload, validates it against the
// claimed chunk scope, and arms the per-run checkpoint compare-and-advance
// inside the generation's own commit transaction. Every other scope is not
// handled and keeps its existing behavior exactly:
//
//   - a baseline generation carrying a window advance verdict is refused
//     fail-closed: a chunk that would move the window cursor commits
//     nothing (refused);
//   - no snapshot, no baseline keys, or should-advance false: held, the
//     facts commit and the checkpoint holds;
//   - a verdict whose stamped identity covers nothing new (blank or
//     malformed seed sha, or an advance at or behind the chunk start):
//     not-covered, the facts commit and the checkpoint holds;
//   - a complete, consistent verdict: advanced, the checkpoint writer is
//     armed to the committed chunk end.
//
// The host cannot re-resolve the seed manifest (it holds no seed handle
// by design, mirroring the window host holding no storage handle), so the
// seed-vs-manifest comparison stays a plan-time check (the stale-seed
// hold): the commit path fails closed on shape and coverage only.
//
// It is safe for concurrent use: it holds only immutable configuration,
// and the checkpoint store's compare-and-advance orders concurrent commits.
type NVDBaselineCommitPolicy struct {
	collectorInstanceID string
	instruments         *telemetry.Instruments
	logger              *slog.Logger
	clock               func() time.Time
}

var _ ClaimCommitPolicy = (*NVDBaselineCommitPolicy)(nil)

// NewNVDBaselineCommitPolicy builds the policy for one collector instance.
// A blank instance id is a construction error: a checkpoint row keyed under
// an invented run would strand the stream. Instruments and logger are
// optional; nil skips the signal.
func NewNVDBaselineCommitPolicy(collectorInstanceID string, instruments *telemetry.Instruments, logger *slog.Logger) (*NVDBaselineCommitPolicy, error) {
	instanceID := strings.TrimSpace(collectorInstanceID)
	if instanceID == "" {
		return nil, errors.New("nvd baseline commit policy requires a collector instance id")
	}
	return &NVDBaselineCommitPolicy{
		collectorInstanceID: instanceID,
		instruments:         instruments,
		logger:              logger,
		clock:               time.Now,
	}, nil
}

// CommitPolicy implements ClaimCommitPolicy for NVD baseline chunk scopes.
// Every other scope is not handled and keeps its existing behavior.
func (p *NVDBaselineCommitPolicy) CommitPolicy(
	item workflow.WorkItem,
	observedAt time.Time,
	envelopes []facts.Envelope,
) (ClaimCommitDecision, error) {
	runID, year, chunkStart, ok := vulnsource.ParseNVDBaselineScope(item.ScopeID)
	if !ok {
		return ClaimCommitDecision{}, nil
	}
	ctx := context.Background()
	record := func(outcome string) {
		p.instruments.RecordNVDBaselineAdvance(ctx, outcome)
	}
	refuse := func(class, outcome string, err error) (ClaimCommitDecision, error) {
		record(outcome)
		if p.logger != nil {
			p.logger.Warn("nvd baseline generation refused by commit policy, nothing committed",
				telemetry.LogKeyFailureClass, class,
				"baseline_run_id", runID,
				"collector_instance_id", p.collectorInstanceID,
				"work_item_id", strings.TrimSpace(item.WorkItemID),
				"generation_id", strings.TrimSpace(item.GenerationID),
				"error", err.Error(),
			)
		}
		return ClaimCommitDecision{Handled: true}, &CommitPolicyRefusal{Class: class, Err: err}
	}
	hold := func(outcome string) (ClaimCommitDecision, error) {
		record(outcome)
		return ClaimCommitDecision{Handled: true}, nil
	}
	payload, found := nvdBaselineSnapshotPayload(envelopes)
	if !found {
		return hold(telemetry.NVDBaselineOutcomeHeld)
	}
	if baselineWindowVerdictWouldArm(payload) {
		return refuse(FailureClassNVDBaselineWindowWriterArmed, telemetry.NVDBaselineOutcomeRefused,
			errors.New("nvd baseline generation carries a window advance verdict; a baseline chunk never arms the window cursor writer"))
	}
	shouldAdvance, _ := payloadBool(payload, nvdBaselineShouldAdvancePayloadKey)
	if !shouldAdvance {
		return hold(telemetry.NVDBaselineOutcomeHeld)
	}
	advanceTo, advanceOK := payloadMicros(payload, nvdBaselineAdvanceToPayloadKey)
	sha, _ := payload[nvdBaselineYearSHAPayloadKey].(string)
	if !advanceOK || advanceTo <= 0 || !isBaselineYearSHA(sha) || advanceTo <= chunkStart.UnixMicro() {
		return hold(telemetry.NVDBaselineOutcomeNotCovered)
	}
	complete, _ := payload[nvdBaselineYearCompletePayloadKey].(bool)
	if observedAt.IsZero() {
		observedAt = p.clock().UTC()
	}
	record(telemetry.NVDBaselineOutcomeAdvanced)
	return ClaimCommitDecision{
		Handled: true,
		StateWriter: vulnerabilitystore.NVDBaselineCheckpointWriter(
			runID, year, advanceTo, strings.TrimSpace(sha), complete, observedAt.UTC()),
	}, nil
}

// baselineWindowVerdictWouldArm reports whether the snapshot payload
// carries a window advance verdict that would arm the NVD window cursor
// writer: should-advance true with a positive advance-to. The mapping's
// arming gate is scope-family-first, so a baseline scope never arms — but
// the commit policy refuses the confused shape fail-closed instead of
// trusting the gate alone.
func baselineWindowVerdictWouldArm(payload map[string]any) bool {
	shouldAdvance, ok := payloadBool(payload, nvdWindowShouldAdvancePayloadKey)
	if !ok || !shouldAdvance {
		return false
	}
	advanceTo, ok := payloadMicros(payload, nvdWindowAdvanceToPayloadKey)
	return ok && advanceTo > 0
}

// nvdBaselineSnapshotPayload returns the raw payload of the first source
// snapshot fact in the generation, or false when none is present. It
// shares the snapshot fact kind with the window verdict path: the
// satellite stamps both verdicts on the same snapshot fact.
func nvdBaselineSnapshotPayload(envelopes []facts.Envelope) (map[string]any, bool) {
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

// isBaselineYearSHA reports whether sha is a well-formed seed identity: 64
// lowercase hex characters. A blank or malformed sha covers nothing the
// next plan can pin, so its generation commits facts but never advances
// the checkpoint.
func isBaselineYearSHA(sha string) bool {
	sha = strings.TrimSpace(sha)
	if len(sha) != 64 {
		return false
	}
	for _, r := range sha {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}
