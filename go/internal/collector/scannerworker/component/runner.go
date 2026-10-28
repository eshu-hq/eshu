// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"

	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability/osruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/imageanalyzer"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/sbomgenerator"
	"github.com/eshu-hq/eshu/go/internal/factenvelope"
)

// protocolVersion is the only collector wire contract these binaries speak.
// It mirrors collector.ProtocolVersionV1Alpha3 in sdk/go/collector; the
// string must stay identical because the pinned eshu-sdk/collector module
// predates the constant.
const protocolVersion = "collector-sdk/v1alpha3"

// Runner executes one analyzer behind the v1alpha3 stdio contract: it reads
// one host request from Stdin and writes exactly one result page document to
// Stdout. The host re-invokes the binary with the page cursor until a final
// page closes the generation; every invocation is stateless and repeats the
// same analysis deterministically.
//
// The binary is an analyzer executor in the same trust domain as the in-tree
// worker, not a claim owner: the host validated the claim, the activation,
// and the producer grants before launch. Claim-derived identity the SDK
// claim does not carry is recomputed deterministically (the locator hash
// from the scope ID) or scoped to the executor (owner and claim-correlation
// IDs name the component, never the workflow); none of it crosses the wire.
// The page cursor carries the claim observation time the first page minted,
// so every page stamps the same Generation.ObservedAt for the host
// generation pin.
type Runner struct {
	// ComponentID is the binary identity (IDImage, IDSBOM, IDOSRuntime).
	// The claim's component ID must equal it.
	ComponentID string
	// Stdin carries one JSON request document.
	Stdin io.Reader
	// Stdout receives exactly one JSON result page document.
	Stdout io.Writer
	// Stderr receives structured log lines. Nil discards.
	Stderr io.Writer
	// Clock overrides the observation clock. Nil uses time.Now().UTC.
	Clock func() time.Time
}

// request mirrors the host wire document at the JSON level. Decoding is
// deliberately liberal (unknown fields ignored) so an older binary keeps
// running against a newer host that adds request fields; emission stays
// strict. The continuation travels only on resumed calls.
type request struct {
	ProtocolVersion string             `json:"protocol_version"`
	Claim           sdkcollector.Claim `json:"claim"`
	Config          map[string]any     `json:"config,omitempty"`
	Continuation    *continuation      `json:"continuation,omitempty"`
}

// continuation carries the resume cursor back from the host.
type continuation struct {
	Cursor string `json:"cursor,omitempty"`
}

// pageEnvelope mirrors the v1alpha3 page continuation the pinned SDK Result
// predates. Field tags must stay identical with collector.ResultPage in
// sdk/go/collector and the host mirror.
type pageEnvelope struct {
	Cursor     string `json:"cursor,omitempty"`
	More       bool   `json:"more,omitempty"`
	PageIndex  int    `json:"page_index,omitempty"`
	TotalFacts int    `json:"total_facts,omitempty"`
}

// wireResult is the exact stdout document: the SDK result plus its page
// envelope. The host decodes it strictly and rejects trailing JSON.
type wireResult struct {
	sdkcollector.Result
	Page *pageEnvelope `json:"page,omitempty"`
}

// Run executes one call: decode, validate, analyze, page, emit.
//
// Error taxonomy (collector-sdk/v1alpha3 pagination, extensionhost
// collectPages): every Go error returned here becomes a host launch failure,
// which is RETRYABLE until the claim lease expires. Deterministic input
// defects therefore emit terminal Results against the echoed claim instead:
// a bad activation fails once with a bounded component_* class rather than
// retry-storming to lease expiry. Go errors remain only where no
// trustworthy result can be constructed: transport failures, undecodable or
// oversized requests (no claim to echo), a foreign protocol version
// (emitting v1alpha3 to a host that speaks something else is undecodable
// output, and version skew may resolve on redeploy, so retryable is
// honest), and an unknown binary identity (deployment wiring no result can
// attribute).
func (r Runner) Run(ctx context.Context) error {
	if r.Stdin == nil {
		return fmt.Errorf("component stdin is required")
	}
	if r.Stdout == nil {
		return fmt.Errorf("component stdout is required")
	}
	callNow := r.clock()
	raw, err := io.ReadAll(io.LimitReader(r.Stdin, maxRequestBytes+1))
	if err != nil {
		return fmt.Errorf("read component request: %w", err)
	}
	if int64(len(raw)) > maxRequestBytes {
		return fmt.Errorf("component request above %d bytes", maxRequestBytes)
	}
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("decode component request: %w", err)
	}
	if req.ProtocolVersion != protocolVersion {
		return fmt.Errorf("component protocol %q is not %q", req.ProtocolVersion, protocolVersion)
	}
	analyzer, err := AnalyzerFor(r.ComponentID)
	if err != nil {
		return err
	}
	// The claim observation time is minted on the first page and travels
	// in the opaque page cursor the host echoes back, so every page of one
	// claim stamps the same Generation.ObservedAt across the stateless
	// re-invocations. Stamping the per-call clock here breaks the host
	// generation pin (extensionhost sameGeneration) on page two.
	observedAt := callNow
	resume := 0
	if req.Continuation != nil {
		var cursorObservedAt time.Time
		resume, cursorObservedAt, err = parseCursor(req.Continuation.Cursor)
		if err != nil {
			r.log(analyzer, "terminal", 0, 0, 0, "component_bad_cursor")
			return r.emitTerminal(req.Claim, callNow, "component_bad_cursor")
		}
		observedAt = cursorObservedAt
	}
	if req.Claim.ComponentID != r.ComponentID {
		r.log(analyzer, "terminal", 0, 0, 0, "component_claim_mismatch")
		return r.emitTerminal(req.Claim, observedAt, "component_claim_mismatch")
	}
	config, err := parseConfig(req.Config)
	if err != nil {
		r.log(analyzer, "terminal", 0, 0, 0, "component_bad_config")
		return r.emitTerminal(req.Claim, observedAt, "component_bad_config")
	}
	input, err := claimInput(req.Claim, config, analyzer, observedAt)
	if err != nil {
		// The echoed claim is malformed, so the host SDK validator will
		// reject this result as invalid: still terminal, still no retry.
		r.log(analyzer, "terminal", 0, 0, 0, "component_bad_claim")
		return r.emitTerminal(req.Claim, observedAt, "component_bad_claim")
	}
	executor, err := buildAnalyzer(analyzer, req.Claim, config, observedAt)
	if err != nil {
		r.log(analyzer, "terminal", 0, 0, 0, "component_failed")
		return r.emitTerminal(req.Claim, observedAt, "component_failed")
	}
	bound, err := pageBound(config.PageBoundBytes)
	if err != nil {
		return err
	}
	start := callNow
	analyzeCtx, cancel := r.analyzeContext(ctx, input)
	result, err := executor.Analyze(analyzeCtx, input)
	cancel()
	latencyMS := int(time.Since(start).Milliseconds())
	if err != nil {
		r.log(analyzer, failureState(err), 0, 0, 0, failureClass(err))
		return r.emitFailure(req.Claim, observedAt, err)
	}
	output := result.Output
	if err := scannerworker.ValidateFactOutput(input, output); err != nil {
		r.log(analyzer, "terminal", 0, 0, 0, "component_invalid_output")
		return r.emitTerminal(req.Claim, observedAt, "component_invalid_output")
	}
	pages, err := scannerworker.SplitFactOutput(output, bound)
	if err != nil {
		r.log(analyzer, "terminal", 0, 0, 0, "component_fact_above_bound")
		return r.emitTerminal(req.Claim, observedAt, "component_fact_above_bound")
	}
	if len(pages) > maxPagesPerClaim {
		r.log(analyzer, "terminal", 0, 0, 0, "component_page_bound_exceeded")
		return r.emitTerminal(req.Claim, observedAt, "component_page_bound_exceeded")
	}
	index := resume
	if req.Continuation != nil {
		if err := checkCursorRange(req.Continuation.Cursor, index, len(pages)); err != nil {
			r.log(analyzer, "terminal", 0, 0, 0, "component_bad_cursor")
			return r.emitTerminal(req.Claim, observedAt, "component_bad_cursor")
		}
	}
	page := pages[index]
	wireFacts := make([]sdkcollector.Fact, 0, len(page.Facts))
	for _, envelope := range page.Facts {
		wire, err := factenvelope.SDKFactFromInternal(envelope)
		if err != nil {
			r.log(analyzer, "terminal", 0, 0, 0, "component_invalid_output")
			return r.emitTerminal(req.Claim, observedAt, "component_invalid_output")
		}
		wireFacts = append(wireFacts, wire)
	}
	envelope := &pageEnvelope{PageIndex: index, TotalFacts: len(output.Facts)}
	if index < len(pages)-1 {
		envelope.Cursor = formatCursor(index+1, observedAt)
		envelope.More = true
	}
	wire := wireResult{
		Result: sdkcollector.Result{
			ProtocolVersion: protocolVersion,
			State:           sdkcollector.ResultComplete,
			Claim:           req.Claim,
			Generation: sdkcollector.Generation{
				ID:            req.Claim.GenerationID,
				ObservedAt:    observedAt,
				FreshnessHint: "snapshot",
			},
			Facts: wireFacts,
			Statuses: []sdkcollector.Status{{
				Class:           sdkcollector.StatusComplete,
				FactCount:       len(wireFacts),
				SourceLatencyMS: latencyMS,
			}},
		},
		Page: envelope,
	}
	raw, err = json.Marshal(wire)
	if err != nil {
		return fmt.Errorf("encode component result: %w", err)
	}
	raw = append(raw, '\n')
	if _, err := io.Copy(r.Stdout, bytes.NewReader(raw)); err != nil {
		return fmt.Errorf("write component result: %w", err)
	}
	r.log(analyzer, "complete", index, len(wireFacts), len(output.Facts), "")
	return nil
}

// clock returns the runner clock in UTC.
func (r Runner) clock() time.Time {
	if r.Clock != nil {
		return r.Clock().UTC()
	}
	return time.Now().UTC()
}

// analyzeContext bounds one analysis by the analyzer resource timeout. The
// host owns the claim lease; this only stops a runaway analyzer from
// holding the call past its budget.
func (r Runner) analyzeContext(ctx context.Context, input scannerworker.ClaimInput) (context.Context, context.CancelFunc) {
	timeout := input.Limits.Timeout
	if timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, timeout)
}

// claimInput assembles the analyzer input from the SDK claim and the
// operator config. Every field derives from claim-carried or
// operator-declared values: the locator hash is recomputed from the scope
// ID (never invented), and the owner/correlation IDs name this executor
// (they never cross the wire and no analyzer reads them into facts).
func claimInput(claim sdkcollector.Claim, config Config, analyzer scannerworker.AnalyzerKind, now time.Time) (scannerworker.ClaimInput, error) {
	if strings.TrimSpace(claim.WorkItemID) == "" {
		return scannerworker.ClaimInput{}, errors.New("claim work_item_id must not be blank")
	}
	if strings.TrimSpace(claim.InstanceID) == "" {
		return scannerworker.ClaimInput{}, errors.New("claim instance_id must not be blank")
	}
	if strings.TrimSpace(claim.SourceRunID) == "" {
		return scannerworker.ClaimInput{}, errors.New("claim source_run_id must not be blank")
	}
	if strings.TrimSpace(claim.GenerationID) == "" {
		return scannerworker.ClaimInput{}, errors.New("claim generation_id must not be blank")
	}
	if strings.TrimSpace(claim.Scope.ID) == "" {
		return scannerworker.ClaimInput{}, errors.New("claim scope_id must not be blank")
	}
	fencing, err := strconv.ParseInt(strings.TrimSpace(claim.FencingToken), 10, 64)
	if err != nil || fencing <= 0 {
		return scannerworker.ClaimInput{}, errors.New("claim fencing_token must be a positive integer")
	}
	target, err := scannerworker.TargetScopeForComponent(claim.Scope.ID, config.AcceptanceUnitID, claim.SourceRunID, claim.GenerationID)
	if err != nil {
		return scannerworker.ClaimInput{}, err
	}
	limits, err := scannerworker.DefaultResourceLimits(analyzer)
	if err != nil {
		return scannerworker.ClaimInput{}, err
	}
	return scannerworker.ClaimInput{
		WorkItemID:     claim.WorkItemID,
		ClaimID:        fmt.Sprintf("%s/%s/attempt-%d", claim.ComponentID, claim.WorkItemID, claim.Attempt),
		FencingToken:   fencing,
		OwnerID:        claim.ComponentID,
		Analyzer:       analyzer,
		Target:         target,
		Limits:         limits,
		GenerationID:   claim.GenerationID,
		Attempt:        claim.Attempt,
		ClaimedAt:      now,
		LeaseExpiresAt: claim.Deadline,
		ObservedAt:     now,
	}, nil
}

// buildAnalyzer constructs the analyzer for one component from the operator
// targets. Targets decode strictly into the analyzer-native shape so the
// binary accepts exactly what the in-tree worker accepts.
func buildAnalyzer(analyzer scannerworker.AnalyzerKind, claim sdkcollector.Claim, config Config, now time.Time) (scannerworker.Analyzer, error) {
	switch analyzer {
	case scannerworker.AnalyzerImageUnpacking:
		var targets []imageanalyzer.TargetConfig
		if err := decodeTargets(config.Targets, &targets); err != nil {
			return nil, err
		}
		return imageanalyzer.NewAnalyzer(imageanalyzer.AnalyzerConfig{
			CollectorInstanceID: claim.InstanceID,
			Targets:             targets,
			Now:                 func() time.Time { return now },
		})
	case scannerworker.AnalyzerOSPackageExtraction:
		var targets []osruntime.TargetConfig
		if err := decodeTargets(config.Targets, &targets); err != nil {
			return nil, err
		}
		return osruntime.NewAnalyzer(osruntime.AnalyzerConfig{
			CollectorInstanceID: claim.InstanceID,
			Targets:             targets,
			Now:                 func() time.Time { return now },
		})
	case scannerworker.AnalyzerSBOMGeneration:
		var targets []sbomgenerator.RepositoryTarget
		if err := decodeTargets(config.Targets, &targets); err != nil {
			return nil, err
		}
		source, err := sbomgenerator.NewRepositorySource(targets)
		if err != nil {
			return nil, err
		}
		return sbomgenerator.Analyzer{Source: source, Now: func() time.Time { return now }}, nil
	default:
		return nil, fmt.Errorf("no component analyzer for %q", analyzer)
	}
}

// decodeTargets decodes raw target documents strictly into the
// analyzer-native shape.
func decodeTargets(raw []json.RawMessage, targets any) error {
	payload, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("encode component targets: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(targets); err != nil {
		return fmt.Errorf("decode component targets: %w", err)
	}
	return nil
}

// pageBound resolves the per-call page byte bound: the operator-narrowed
// value when configured, else the stdout-derived default both sides mirror.
// A configured bound above the default is clamped to it: pages must fit the
// host stdout limit, so widening can only buy a truncation plus retry loop,
// never more facts per call.
func pageBound(configured int64) (int64, error) {
	def, err := pageBoundForStdout(DefaultStdoutLimitBytes)
	if err != nil {
		return 0, err
	}
	if configured == 0 {
		return def, nil
	}
	if configured < 0 {
		return 0, fmt.Errorf("page_bound_bytes must not be negative")
	}
	if configured > def {
		return def, nil
	}
	return configured, nil
}

// failureClass maps an analysis error to a bounded failure class for the
// status record. Analyzer failures carry the vocabulary class; anything else
// is an undifferentiated component failure. Values are never interpolated:
// raw causes can carry repository paths, image names, or scope IDs.
func failureClass(err error) string {
	var failure scannerworker.AnalyzerFailure
	if errors.As(err, &failure) {
		return string(failure.FailureClass())
	}
	return "component_failed"
}

// failureState maps an analysis error to the claimed result state:
// retryable analyzer failures release the claim for a bounded retry,
// everything else fails terminal.
func failureState(err error) sdkcollector.ResultState {
	var failure scannerworker.AnalyzerFailure
	if errors.As(err, &failure) && failure.Disposition() == scannerworker.FailureRetryable {
		return sdkcollector.ResultRetryable
	}
	return sdkcollector.ResultTerminal
}

// emitFailure emits a single-shot failure result for an analysis error.
func (r Runner) emitFailure(claim sdkcollector.Claim, observedAt time.Time, err error) error {
	return r.emit(claim, observedAt, failureState(err), nil, []sdkcollector.Status{{
		Class:        sdkcollector.StatusFailure,
		FailureClass: failureClass(err),
	}}, nil)
}

// emitTerminal emits a single-shot terminal result for a binary-side defect
// (invalid output, unpageable fact) or a deterministic input defect (bad
// config, malformed claim, forged cursor): retry cannot fix it, so the
// claim must not be released for another attempt. The class names the
// defect, never the values that triggered it.
func (r Runner) emitTerminal(claim sdkcollector.Claim, observedAt time.Time, class string) error {
	return r.emit(claim, observedAt, sdkcollector.ResultTerminal, nil, []sdkcollector.Status{{
		Class:        sdkcollector.StatusFailure,
		FailureClass: class,
	}}, nil)
}

// emit writes one result document to stdout.
func (r Runner) emit(claim sdkcollector.Claim, observedAt time.Time, state sdkcollector.ResultState, facts []sdkcollector.Fact, statuses []sdkcollector.Status, page *pageEnvelope) error {
	raw, err := json.Marshal(wireResult{
		Result: sdkcollector.Result{
			ProtocolVersion: protocolVersion,
			State:           state,
			Claim:           claim,
			Generation: sdkcollector.Generation{
				ID:            claim.GenerationID,
				ObservedAt:    observedAt,
				FreshnessHint: "snapshot",
			},
			Facts:    facts,
			Statuses: statuses,
		},
		Page: page,
	})
	if err != nil {
		return fmt.Errorf("encode component result: %w", err)
	}
	raw = append(raw, '\n')
	if _, err := io.Copy(r.Stdout, bytes.NewReader(raw)); err != nil {
		return fmt.Errorf("write component result: %w", err)
	}
	return nil
}

// log writes one structured stderr line per call. It carries the component
// identity, outcome, and counts only: claim values (scope IDs, image
// references, paths, digests) never appear in logs.
func (r Runner) log(analyzer scannerworker.AnalyzerKind, state sdkcollector.ResultState, page, facts, total int, class string) {
	if r.Stderr == nil {
		return
	}
	record := map[string]any{
		"component": r.ComponentID,
		"analyzer":  string(analyzer),
		"state":     string(state),
		"page":      page,
		"facts":     facts,
		"total":     total,
		"at":        r.clock().Format(time.RFC3339Nano),
	}
	if class != "" {
		record["failure_class"] = class
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return
	}
	raw = append(raw, '\n')
	_, _ = r.Stderr.Write(raw)
}
