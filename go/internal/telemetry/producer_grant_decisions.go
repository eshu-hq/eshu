// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// producerGrantSetCacheCap bounds the option cache. Every key component
// is closed or registry-bounded (4 stages x 2 decisions x 8 reasons x the core
// fact-kind registry plus "other"), so the cache stays far below this in
// practice. The cache is keyed on the raw caller values (folding happens once
// per key), so the cap is the backstop that keeps a caller passing arbitrary
// strings from growing memory without bound. Past it, options are built per call (correct, allocating).
const producerGrantSetCacheCap = 1 << 14

type producerGrantKey struct {
	stage, decision, reason, factKind string
}

// ProducerGrantDecisionRecorder records producer-grant decisions on the
// eshu_dp_component_producer_grant_decisions_total counter. The per-emission
// recheck records on every extension result, so it pre-builds and caches one
// add option (attribute set) per distinct label combination: a warmed-up Record performs a
// read-locked map lookup and one counter Add, with no allocation.
//
// Every label value is folded into its closed vocabulary before use (see
// contract.ProducerGrant*), so no caller-supplied string can create a new time
// series. It is safe for concurrent use. A nil recorder, or one built without
// instruments, records nothing.
type ProducerGrantDecisionRecorder struct {
	counter metric.Int64Counter
	mu      sync.RWMutex
	opts    map[producerGrantKey]metric.AddOption
}

// NewProducerGrantDecisionRecorder builds a recorder over inst. A nil inst
// yields a recorder whose Record is a no-op.
func NewProducerGrantDecisionRecorder(inst *Instruments) *ProducerGrantDecisionRecorder {
	if inst == nil || inst.ProducerGrantDecisions == nil {
		return &ProducerGrantDecisionRecorder{}
	}
	return &ProducerGrantDecisionRecorder{
		counter: inst.ProducerGrantDecisions,
		opts:    make(map[producerGrantKey]metric.AddOption),
	}
}

// Record adds one producer-grant decision. stage, decision, and reason must be
// contract.ProducerGrant* values and factKind a registered core fact kind;
// anything else is folded to "unknown" / "other".
func (r *ProducerGrantDecisionRecorder) Record(ctx context.Context, stage, decision, reason, factKind string) {
	if r == nil || r.counter == nil {
		return
	}
	r.counter.Add(ctx, 1, r.addOption(producerGrantKey{
		stage: stage, decision: decision, reason: reason, factKind: factKind,
	}))
}

// addOption returns the cached add option for the raw caller-supplied key,
// folding and caching it on first use. Folding (notably the fact-kind registry
// lookup, which copies the registry) happens once per distinct raw key, so the
// hot path is a read-locked map lookup and allocates nothing of its own.
func (r *ProducerGrantDecisionRecorder) addOption(key producerGrantKey) metric.AddOption {
	r.mu.RLock()
	opt, ok := r.opts[key]
	r.mu.RUnlock()
	if ok {
		return opt
	}
	opt = metric.WithAttributeSet(attribute.NewSet(
		attribute.String(MetricDimensionDecision, foldGrantDecision(key.decision)),
		attribute.String(MetricDimensionStage, foldGrantStage(key.stage)),
		attribute.String(MetricDimensionReason, foldGrantReason(key.reason)),
		attribute.String(MetricDimensionFactKind, foldGrantFactKind(key.factKind)),
	))
	r.mu.Lock()
	if len(r.opts) < producerGrantSetCacheCap {
		r.opts[key] = opt
	}
	r.mu.Unlock()
	return opt
}

func foldGrantStage(v string) string {
	switch v {
	case contract.ProducerGrantStageInstall, contract.ProducerGrantStageReadback,
		contract.ProducerGrantStageActivation, contract.ProducerGrantStageEmission:
		return v
	}
	return contract.ProducerGrantReasonUnknown
}

func foldGrantDecision(v string) string {
	switch v {
	case contract.ProducerGrantDecisionAllow, contract.ProducerGrantDecisionDeny:
		return v
	}
	return contract.ProducerGrantReasonUnknown
}

func foldGrantReason(v string) string {
	switch v {
	case contract.ProducerGrantReasonGranted, contract.ProducerGrantReasonNoMatchingGrant,
		contract.ProducerGrantReasonRevoked, contract.ProducerGrantReasonExpired,
		contract.ProducerGrantReasonScopeMismatch, contract.ProducerGrantReasonSchemaNotCovered,
		contract.ProducerGrantReasonGrantsUnreadable:
		return v
	}
	return contract.ProducerGrantReasonUnknown
}

func foldGrantFactKind(v string) string {
	if facts.IsCoreFactKind(v) {
		return v
	}
	return contract.ProducerGrantFactKindOther
}
