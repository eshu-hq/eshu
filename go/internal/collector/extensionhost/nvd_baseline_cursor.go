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
	// nvdBaselineAdvanceToCVEIDPayloadKey is the CVE ID half of the resume
	// position advance_to names (eshu-hq/eshu#10809): the first record not
	// emitted, in canonical form. Absent or empty means the start of the
	// advance_to instant, which is also what a collector that predates the key
	// stamps, so the commit stays safe against an old collector.
	nvdBaselineAdvanceToCVEIDPayloadKey = "nvd_baseline_advance_to_cve_id"
	// nvdBaselineYearGeneratedPayloadKey is the seed manifest's lastModified
	// for the year in Unix microseconds, stamped beside the sha
	// (eshu-hq/eshu#10810). The checkpoint writer orders seed rotation by it
	// inside the commit transaction. Absent means the collector does not
	// stamp it and the rollback guard stays out of the way. Both new keys
	// mirror the shared verdict vector in eshu-sdk (eshu-hq/eshu-sdk#97); the
	// pin bump that replays it lands separately.
	nvdBaselineYearGeneratedPayloadKey = "nvd_baseline_year_generated_unix_us"
)

// FailureClassNVDBaselineSeedRollback marks a baseline generation whose commit
// transaction found the stored seed newer than the chunk's (#10810). The
// whole generation rolls back, the claim fails retryable with this class
// instead of commit_failure, and the next plan re-mints the same position.
const FailureClassNVDBaselineSeedRollback = vulnerabilitystore.FailureClassNVDBaselineSeedRollback

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
//     malformed seed sha, a malformed resume CVE ID or seed stamp, a
//     position behind the chunk start, or an incomplete advance that does
//     not move strictly past it): not-covered, the facts commit and the
//     checkpoint holds. The position is the pair (advance_to, advance_to
//     CVE ID) compared as a row, so a chunk that cuts inside one instant
//     advances by its CVE ID. A complete verdict may land exactly on the
//     chunk start (see nvdBaselinePositionCovers);
//   - a consistent verdict: advanced, the checkpoint writer is armed to
//     the committed chunk end, floored to a whole UTC millisecond.
//
// The host cannot re-resolve the seed manifest (it holds no seed handle
// by design, mirroring the window host holding no storage handle), so seed
// rotation is ordered where the stored seed is visible: the checkpoint
// writer compares the stamped seed generation with the stored one inside the
// commit transaction. A newer seed resumes (forward rotation); an older one,
// or the same stamp under another sha, fails the claim with class
// nvd_baseline_seed_rollback and rolls back the whole generation (#10810).
//
// It is safe for concurrent use: it holds only immutable configuration,
// and the checkpoint store's compare-and-advance orders concurrent commits.
type NVDBaselineCommitPolicy struct {
	collectorInstanceID string
	instruments         *telemetry.Instruments
	logger              *slog.Logger
	clock               func() time.Time
	// settlements holds each armed chunk's facts for the INFO line and the
	// forward-rotation count until the commit lands.
	settlements *baselineSettlements
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
		settlements:         newBaselineSettlements(),
	}, nil
}

// CommitPolicy implements ClaimCommitPolicy for NVD baseline chunk scopes.
// Every other scope is not handled and keeps its existing behavior.
func (p *NVDBaselineCommitPolicy) CommitPolicy(
	item workflow.WorkItem,
	observedAt time.Time,
	envelopes []facts.Envelope,
) (ClaimCommitDecision, error) {
	runID, year, chunkStart, chunkFromCVEID, ok := vulnsource.ParseNVDBaselineScope(item.ScopeID)
	if !ok {
		return ClaimCommitDecision{}, nil
	}
	// A retry of this item starts a new decision: drop what an earlier
	// attempt that never committed left behind.
	p.settlements.discard(item)
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
	complete, _ := payload[nvdBaselineYearCompletePayloadKey].(bool)
	advanceID := ""
	if raw, found := payload[nvdBaselineAdvanceToCVEIDPayloadKey]; found && raw != nil {
		id, isString := raw.(string)
		if !isString {
			// A non-string id is a malformed position; the sentinel is not
			// canonical, so the reason check below names it.
			id = "?"
		}
		advanceID = id
	}
	generated, generatedOK := int64(0), true
	if _, found := payload[nvdBaselineYearGeneratedPayloadKey]; found {
		generated, generatedOK = payloadMicros(payload, nvdBaselineYearGeneratedPayloadKey)
	}
	rawAdvanceTo := advanceTo
	if advanceOK && advanceTo > 0 {
		// Whole UTC milliseconds, the NVD wire form the scope id and the
		// claim start also carry. A sub-millisecond stamp (collector#123
		// reads one millisecond back) floors, never rounds up, so the
		// stored resume can only re-read, never skip.
		advanceTo = vulnsource.NormalizeNVDWindowTime(time.UnixMicro(advanceTo)).UnixMicro()
	}
	start := nvdBaselinePosition{unixMicro: chunkStart.UnixMicro(), cveID: chunkFromCVEID}
	end := nvdBaselinePosition{unixMicro: advanceTo, cveID: advanceID}
	if reason := nvdBaselineNotCoveredReason(advanceOK, end, rawAdvanceTo, sha, generated, generatedOK, start, complete); reason != "" {
		if p.logger != nil {
			p.logger.Warn(nvdBaselineNotCoveredLogMessage,
				"reason", reason,
				"baseline_run_id", runID,
				"collector_instance_id", p.collectorInstanceID,
				"work_item_id", strings.TrimSpace(item.WorkItemID),
				"generation_id", strings.TrimSpace(item.GenerationID),
			)
		}
		return hold(telemetry.NVDBaselineOutcomeNotCovered)
	}
	if observedAt.IsZero() {
		observedAt = p.clock().UTC()
	}
	record(telemetry.NVDBaselineOutcomeAdvanced)
	advance := vulnerabilitystore.NVDBaselineAdvance{
		BaselineRunID:          runID,
		Year:                   year,
		ResumeUnixMicro:        advanceTo,
		ResumeCVEID:            advanceID,
		YearSHA256:             strings.TrimSpace(sha),
		YearGeneratedUnixMicro: generated,
		YearComplete:           complete,
		ScopeID:                item.ScopeID,
		GenerationID:           strings.TrimSpace(item.GenerationID),
		ObservedAt:             observedAt.UTC(),
	}
	return ClaimCommitDecision{
		Handled:     true,
		StateWriter: p.checkpointWriter(item, advance, len(envelopes)),
	}, nil
}

// nvdBaselineNotCoveredLogMessage is the WARN message logged once per
// generation the commit policy holds as not_covered. Its bounded reason
// attribute is one of the nvdBaselineNotCoveredReason* values.
const nvdBaselineNotCoveredLogMessage = "nvd baseline generation not covered by commit policy, checkpoint holds"

// Closed reason set of the not_covered WARN. The reasons are checked in this
// order, so a verdict with several defects logs the first.
const (
	// nvdBaselineNotCoveredReasonAdvanceMissing: advance_to absent, not a
	// number, or not positive.
	nvdBaselineNotCoveredReasonAdvanceMissing = "advance_missing"
	// nvdBaselineNotCoveredReasonSHAMalformed: the seed sha is blank or, after
	// trimming surrounding whitespace, not 64 lowercase hex characters.
	nvdBaselineNotCoveredReasonSHAMalformed = "sha_malformed"
	// nvdBaselineNotCoveredReasonPositionMalformed: the advance_to CVE ID is
	// not canonical, or names a CVE ID inside an advance_to that is not a
	// whole UTC millisecond (the CVE ID half is only meaningful on the whole
	// instant the scope id carries).
	nvdBaselineNotCoveredReasonPositionMalformed = "position_malformed"
	// nvdBaselineNotCoveredReasonGeneratedMalformed: the seed generation stamp
	// is present but not a non-negative number.
	nvdBaselineNotCoveredReasonGeneratedMalformed = "generated_malformed"
	// nvdBaselineNotCoveredReasonAdvanceBehindStart: the resume position is
	// behind the chunk start (any verdict).
	nvdBaselineNotCoveredReasonAdvanceBehindStart = "advance_behind_start"
	// nvdBaselineNotCoveredReasonAdvanceNotPastStart: an incomplete verdict
	// whose position equals the chunk start.
	nvdBaselineNotCoveredReasonAdvanceNotPastStart = "advance_not_past_start"
)

// nvdBaselinePosition is a resume position: the whole-millisecond stamp in
// Unix microseconds and the canonical CVE ID, empty for the start of the
// instant. Positions order as the row (unixMicro, cveID) in byte order, the
// order the collector cuts a chunk in and the checkpoint stores.
type nvdBaselinePosition struct {
	unixMicro int64
	cveID     string
}

// compare orders two positions: negative when p is before other, zero when
// equal, positive when after.
func (p nvdBaselinePosition) compare(other nvdBaselinePosition) int {
	switch {
	case p.unixMicro < other.unixMicro:
		return -1
	case p.unixMicro > other.unixMicro:
		return 1
	}
	return strings.Compare(p.cveID, other.cveID)
}

// nvdBaselineNotCoveredReason names why a stamped verdict covers nothing
// new, or returns "" when it is consistent. end is the already floored
// position and rawAdvanceTo the stamp as the satellite sent it. It mirrors
// nvdBaselinePositionCovers, split by cause so the not_covered counter has a
// diagnosable log beside it.
func nvdBaselineNotCoveredReason(
	advanceOK bool,
	end nvdBaselinePosition,
	rawAdvanceTo int64,
	sha string,
	generated int64,
	generatedOK bool,
	start nvdBaselinePosition,
	complete bool,
) string {
	switch {
	case !advanceOK || end.unixMicro <= 0:
		return nvdBaselineNotCoveredReasonAdvanceMissing
	case !isBaselineYearSHA(sha):
		return nvdBaselineNotCoveredReasonSHAMalformed
	case end.cveID != "" && (!vulnsource.IsCanonicalNVDBaselineCVEID(end.cveID) || rawAdvanceTo != end.unixMicro):
		return nvdBaselineNotCoveredReasonPositionMalformed
	case !generatedOK || generated < 0:
		return nvdBaselineNotCoveredReasonGeneratedMalformed
	case end.compare(start) < 0:
		return nvdBaselineNotCoveredReasonAdvanceBehindStart
	case !nvdBaselinePositionCovers(end, start, complete):
		return nvdBaselineNotCoveredReasonAdvanceNotPastStart
	}
	return ""
}

// nvdBaselinePositionCovers applies the advance_to contract: the position is
// an inclusive lower bound naming the first record not emitted, so an
// incomplete verdict must move strictly past the chunk start in (stamp, CVE
// ID) order; anything else would re-mint the same scope forever. A complete
// verdict may land exactly on the start: a chunk that resumes at the stream
// tail finds nothing left and has no later position to name, but still
// carries the one fact the planner needs (the year is whole). No verdict may
// move behind the start (#10761, #10809).
func nvdBaselinePositionCovers(end, start nvdBaselinePosition, complete bool) bool {
	order := end.compare(start)
	if order > 0 {
		return true
	}
	return complete && order == 0
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
// lowercase hex characters once surrounding whitespace is trimmed. A blank
// or malformed sha covers nothing the next plan can pin, so its generation
// commits facts but never advances the checkpoint.
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
