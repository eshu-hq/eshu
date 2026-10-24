// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"regexp"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// extensionOptionCacheCap bounds each attribute-set cache. Keys are raw caller
// values, so the cap is the backstop that keeps arbitrary input from growing
// memory; past it options are built per call (correct, allocating).
const extensionOptionCacheCap = 1 << 12

// extensionFailureClassPattern mirrors the extension host's bounded
// failure-class rule so a value that bypassed the host still cannot carry
// spaces, URLs, or mixed case into a label.
var extensionFailureClassPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,95}$`)

type extensionResultKey struct{ kind, state, failureClass string }

type extensionStatusKey struct{ kind, state, statusClass, failureClass string }

type extensionKindStateKey struct{ kind, state string }

// ComponentExtensionStatusRecorder records component-extension status rows
// and claim results on the #8908 instruments. It caches one measurement
// option (attribute set) per distinct label combination, so a warmed call is a
// read-locked map lookup plus the instrument calls, with no allocation of its
// own. Every label is folded into its bounded vocabulary (see
// contract/component_extension.go) before it reaches a series. It is safe for
// concurrent use. A nil recorder, or one built without instruments, records
// nothing.
type ComponentExtensionStatusRecorder struct {
	results    metric.Int64Counter
	statuses   metric.Int64Counter
	facts      metric.Int64Counter
	warnings   metric.Int64Counter
	latency    metric.Float64Histogram
	retryAfter metric.Float64Gauge

	mu             sync.RWMutex
	resultOpts     map[extensionResultKey]metric.MeasurementOption
	statusOpts     map[extensionStatusKey]metric.MeasurementOption
	kindStateOpts  map[extensionKindStateKey]metric.MeasurementOption
	kindOpts       map[string]metric.MeasurementOption
	failureClasses map[string]struct{}
}

// NewComponentExtensionStatusRecorder builds a recorder over inst. A nil inst
// yields a recorder that records nothing.
func NewComponentExtensionStatusRecorder(inst *Instruments) *ComponentExtensionStatusRecorder {
	if inst == nil || inst.ComponentExtensionResults == nil {
		return &ComponentExtensionStatusRecorder{}
	}
	return &ComponentExtensionStatusRecorder{
		results:        inst.ComponentExtensionResults,
		statuses:       inst.ComponentExtensionStatusRecords,
		facts:          inst.ComponentExtensionReportedFacts,
		warnings:       inst.ComponentExtensionReportedWarnings,
		latency:        inst.ComponentExtensionSourceLatency,
		retryAfter:     inst.ComponentExtensionRetryAfter,
		resultOpts:     make(map[extensionResultKey]metric.MeasurementOption),
		statusOpts:     make(map[extensionStatusKey]metric.MeasurementOption),
		kindStateOpts:  make(map[extensionKindStateKey]metric.MeasurementOption),
		kindOpts:       make(map[string]metric.MeasurementOption),
		failureClasses: make(map[string]struct{}),
	}
}

// RecordStatus records one validated SDK status row: one status-record
// increment, plus the reported fact and warning counts and the source latency
// when the row reports them (a zero value is "not reported" and records
// nothing).
func (r *ComponentExtensionStatusRecorder) RecordStatus(
	ctx context.Context,
	collectorKind, state, statusClass, failureClass string,
	factCount, warningCount, sourceLatencyMS int,
) {
	if r == nil || r.results == nil {
		return
	}
	r.statuses.Add(ctx, 1, r.statusOption(extensionStatusKey{
		kind: collectorKind, state: state, statusClass: statusClass, failureClass: failureClass,
	}))
	if factCount <= 0 && warningCount <= 0 && sourceLatencyMS <= 0 {
		return
	}
	opt := r.kindStateOption(extensionKindStateKey{kind: collectorKind, state: state})
	if factCount > 0 {
		r.facts.Add(ctx, int64(factCount), opt)
	}
	if warningCount > 0 {
		r.warnings.Add(ctx, int64(warningCount), opt)
	}
	if sourceLatencyMS > 0 {
		r.latency.Record(ctx, float64(sourceLatencyMS)/1000, opt)
	}
}

// RecordResult records one claim result and sets the retry-after gauge for
// the collector kind to the delay the extension requested, after the one-hour
// cap (zero for a result that is not retryable), so the gauge reports the
// latest request rather than the last throttle forever. It is the request, not
// the applied delay: the claim service applies the larger of the poll interval
// and this value, and none once the attempt budget is spent.
func (r *ComponentExtensionStatusRecorder) RecordResult(
	ctx context.Context,
	collectorKind, state, failureClass string,
	retryAfter time.Duration,
) {
	if r == nil || r.results == nil {
		return
	}
	r.results.Add(ctx, 1, r.resultOption(extensionResultKey{
		kind: collectorKind, state: state, failureClass: failureClass,
	}))
	r.retryAfter.Record(ctx, max(retryAfter, 0).Seconds(), r.kindOption(collectorKind))
}

func (r *ComponentExtensionStatusRecorder) resultOption(key extensionResultKey) metric.MeasurementOption {
	r.mu.RLock()
	opt, ok := r.resultOpts[key]
	r.mu.RUnlock()
	if ok {
		return opt
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if opt, ok := r.resultOpts[key]; ok {
		return opt
	}
	opt = metric.WithAttributeSet(attribute.NewSet(
		AttrCollectorKind(foldExtensionKind(key.kind)),
		AttrState(foldExtensionState(key.state)),
		AttrFailureClass(r.foldFailureClassLocked(key.failureClass)),
	))
	if len(r.resultOpts) < extensionOptionCacheCap {
		r.resultOpts[key] = opt
	}
	return opt
}

func (r *ComponentExtensionStatusRecorder) statusOption(key extensionStatusKey) metric.MeasurementOption {
	r.mu.RLock()
	opt, ok := r.statusOpts[key]
	r.mu.RUnlock()
	if ok {
		return opt
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if opt, ok := r.statusOpts[key]; ok {
		return opt
	}
	opt = metric.WithAttributeSet(attribute.NewSet(
		AttrCollectorKind(foldExtensionKind(key.kind)),
		AttrState(foldExtensionState(key.state)),
		attribute.String(MetricDimensionStatusClass, foldExtensionStatusClass(key.statusClass)),
		AttrFailureClass(r.foldFailureClassLocked(key.failureClass)),
	))
	if len(r.statusOpts) < extensionOptionCacheCap {
		r.statusOpts[key] = opt
	}
	return opt
}

func (r *ComponentExtensionStatusRecorder) kindStateOption(key extensionKindStateKey) metric.MeasurementOption {
	r.mu.RLock()
	opt, ok := r.kindStateOpts[key]
	r.mu.RUnlock()
	if ok {
		return opt
	}
	opt = metric.WithAttributeSet(attribute.NewSet(
		AttrCollectorKind(foldExtensionKind(key.kind)),
		AttrState(foldExtensionState(key.state)),
	))
	r.mu.Lock()
	if len(r.kindStateOpts) < extensionOptionCacheCap {
		r.kindStateOpts[key] = opt
	}
	r.mu.Unlock()
	return opt
}

func (r *ComponentExtensionStatusRecorder) kindOption(kind string) metric.MeasurementOption {
	r.mu.RLock()
	opt, ok := r.kindOpts[kind]
	r.mu.RUnlock()
	if ok {
		return opt
	}
	opt = metric.WithAttributeSet(attribute.NewSet(AttrCollectorKind(foldExtensionKind(kind))))
	r.mu.Lock()
	if len(r.kindOpts) < extensionOptionCacheCap {
		r.kindOpts[kind] = opt
	}
	r.mu.Unlock()
	return opt
}

// foldFailureClassLocked maps a failure class to its label value. It must run
// with r.mu held for writing because admission mutates failureClasses. A class
// admitted once keeps its own label for the life of the process.
func (r *ComponentExtensionStatusRecorder) foldFailureClassLocked(value string) string {
	if value == "" {
		return contract.ExtensionFailureClassNone
	}
	if !extensionFailureClassPattern.MatchString(value) {
		return contract.ExtensionFailureClassOther
	}
	if _, ok := r.failureClasses[value]; ok {
		return value
	}
	if len(r.failureClasses) >= contract.ComponentExtensionFailureClassCap {
		return contract.ExtensionFailureClassOther
	}
	r.failureClasses[value] = struct{}{}
	return value
}

// foldExtensionKind keeps a manifest-bounded collector kind and folds an empty
// or unpatterned one to unknown. The host passes only manifest kinds.
func foldExtensionKind(value string) string {
	if value == "" || !extensionFailureClassPattern.MatchString(value) {
		return contract.ExtensionLabelUnknown
	}
	return value
}

func foldExtensionState(value string) string {
	switch value {
	case contract.ExtensionResultStateComplete, contract.ExtensionResultStateUnchanged,
		contract.ExtensionResultStatePartial, contract.ExtensionResultStateRetryable,
		contract.ExtensionResultStateTerminal:
		return value
	}
	return contract.ExtensionLabelUnknown
}

func foldExtensionStatusClass(value string) string {
	switch value {
	case contract.ExtensionStatusClassProgress, contract.ExtensionStatusClassWarning,
		contract.ExtensionStatusClassFailure, contract.ExtensionStatusClassComplete:
		return value
	}
	return contract.ExtensionLabelUnknown
}
