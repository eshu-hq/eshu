// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workloadinstance

import (
	"context"
	"log/slog"
	"strings"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossscope"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// anchorKeySeparator joins an anchor's workload id and environment in a
// ledger key. The ASCII unit separator does not occur in either value.
const anchorKeySeparator = "\x1f"

// missingAnchorLogSample bounds the missing anchors one log line carries.
const missingAnchorLogSample = 10

// AnchorKey encodes an anchor as a readiness-wait ledger key.
func AnchorKey(anchor Anchor) string {
	return anchor.WorkloadID + anchorKeySeparator + anchor.Environment
}

// ParseAnchorKey decodes a ledger key written by AnchorKey.
func ParseAnchorKey(key string) (Anchor, bool) {
	workloadID, environment, ok := strings.Cut(key, anchorKeySeparator)
	if !ok {
		return Anchor{}, false
	}
	return Anchor{WorkloadID: workloadID, Environment: environment}, true
}

// Wait is the USES handler's commit-first WorkloadInstance wait (#6785). The
// handler reads the ledger row, tries Poll, and otherwise loads its facts,
// calls Evaluate, commits when the decision says so, and then calls Finish.
type Wait struct {
	// Lookup reports which anchors have a WorkloadInstance. Nil disables the
	// wait: every evaluation commits with nothing missing (test wiring).
	Lookup ExistenceLookup
	// Ledger is the (scope, domain) readiness-wait ledger. Nil treats every
	// evaluation as the first of its queue cycle (test wiring).
	Ledger crossscope.ReadinessWaitLedger
	// MaxWait bounds the wait since the first defer. Zero means
	// crossscope.ProducerReadinessMaxWait.
	MaxWait time.Duration
	// Now is the clock. Nil means time.Now.
	Now func() time.Time
}

// Evaluation is one evaluation's wait answer.
type Evaluation struct {
	Decision crossscope.WaitDecision
	// Missing holds the anchors still without a WorkloadInstance, sorted.
	Missing []Anchor
	// Existing holds the anchors whose WorkloadInstance exists.
	Existing map[Anchor]struct{}
}

const waitDomain = reducercontract.DomainWorkloadCloudRelationshipMaterialization

func (w Wait) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

// EffectiveMaxWait returns MaxWait, or the shared default when unset.
func (w Wait) EffectiveMaxWait() time.Duration {
	if w.MaxWait > 0 {
		return w.MaxWait
	}
	return crossscope.ProducerReadinessMaxWait
}

// Read returns the ledger row for the intent's scope. It reads nothing when
// the lookup is nil, because no wait is possible then.
func (w Wait) Read(ctx context.Context, intent reducercontract.Intent) (crossscope.ReadinessWait, bool, error) {
	if w.Lookup == nil {
		return crossscope.ReadinessWait{}, false, nil
	}
	return crossscope.ReadWait(ctx, w.Ledger, intent.ScopeID, waitDomain, crossscope.ReadinessCycleAnchor(intent))
}

// Poll is the cheap poll path. When this generation's rows already committed
// at the ledger's missing set, it looks up only the missing anchors, with no
// fact load and no extraction. handled is false when an anchor appeared (or
// the row is not poll-eligible), and the caller then runs the full
// evaluation, which re-commits. For a handled poll the caller commits nothing
// and then calls Finish.
func (w Wait) Poll(
	ctx context.Context,
	intent reducercontract.Intent,
	existing crossscope.ReadinessWait,
	found bool,
) (Evaluation, bool, error) {
	if w.Lookup == nil || w.Ledger == nil ||
		!crossscope.PollEligible(existing, found, intent.GenerationID, intent.CycleStartedAt) {
		return Evaluation{}, false, nil
	}
	anchors := make([]Anchor, 0, len(existing.MissingKeys))
	for _, key := range existing.MissingKeys {
		anchor, ok := ParseAnchorKey(key)
		if !ok {
			return Evaluation{}, false, nil
		}
		anchors = append(anchors, anchor)
	}
	checked, err := Check(ctx, w.Lookup, anchors)
	if err != nil {
		return Evaluation{}, true, err
	}
	keys := anchorKeys(checked.Missing)
	if !crossscope.SameMissingSet(existing, keys) {
		return Evaluation{}, false, nil
	}
	eval := Evaluation{Missing: checked.Missing, Existing: checked.Existing}
	eval.Decision = crossscope.DecideWait(w.input(intent, existing, true, keys))
	if eval.Decision.Commit {
		return Evaluation{}, false, nil
	}
	return eval, true, nil
}

// Evaluate checks the anchors of a full evaluation and decides whether to
// commit, defer, or settle.
func (w Wait) Evaluate(
	ctx context.Context,
	intent reducercontract.Intent,
	existing crossscope.ReadinessWait,
	found bool,
	anchors []Anchor,
) (Evaluation, error) {
	checked, err := Check(ctx, w.Lookup, anchors)
	if err != nil {
		return Evaluation{}, err
	}
	eval := Evaluation{Missing: checked.Missing, Existing: checked.Existing}
	eval.Decision = crossscope.DecideWait(w.input(intent, existing, found, anchorKeys(checked.Missing)))
	return eval, nil
}

// Finish writes the ledger side of an evaluation. Call it only after the
// commit the decision asked for succeeded.
func (w Wait) Finish(ctx context.Context, intent reducercontract.Intent, eval Evaluation) error {
	return crossscope.ApplyWaitDecision(ctx, w.Ledger, eval.Decision, intent.ScopeID, waitDomain)
}

// LogWait writes the structured log line for an evaluation with a missing
// set. It carries elapsed time since the first defer against the bound, never
// attempt_count, which the non-counting class freezes.
func (w Wait) LogWait(ctx context.Context, intent reducercontract.Intent, eval Evaluation, committed bool) {
	if eval.Decision.Outcome == "" {
		return
	}
	attrs := []any{
		log.ScopeID(intent.ScopeID), log.GenerationID(intent.GenerationID),
		log.FailureClass(NotReadyFailureClass),
		slog.String("readiness_wait_outcome", eval.Decision.Outcome),
		slog.Int("missing_anchor_count", len(eval.Missing)),
		slog.Bool("committed", committed),
		slog.Duration("elapsed_since_first_defer", eval.Decision.Elapsed),
		slog.Duration("max_wait", w.EffectiveMaxWait()),
	}
	switch eval.Decision.Outcome {
	case crossscope.ReadinessWaitDeferred:
		slog.InfoContext(ctx, "workload cloud relationship waiting on workload instances", attrs...)
	case crossscope.ReadinessWaitAbandoned:
		sample := make([]string, 0, missingAnchorLogSample)
		for _, anchor := range eval.Missing {
			if len(sample) == missingAnchorLogSample {
				break
			}
			sample = append(sample, anchor.WorkloadID+"@"+anchor.Environment)
		}
		slog.WarnContext(ctx, "workload cloud relationship instance wait expired; missing workload instances stay unlinked",
			append(attrs, slog.Any("missing_anchor_sample", sample))...)
	default:
		slog.InfoContext(ctx, "workload cloud relationship committed with a settled missing instance set", attrs...)
	}
}

func (w Wait) input(intent reducercontract.Intent, existing crossscope.ReadinessWait, found bool, keys []string) crossscope.WaitInput {
	return crossscope.WaitInput{
		Existing:       existing,
		Found:          found,
		ScopeID:        intent.ScopeID,
		Domain:         waitDomain,
		GenerationID:   intent.GenerationID,
		CycleStartedAt: intent.CycleStartedAt,
		Missing:        keys,
		Now:            w.now(),
		MaxWait:        w.MaxWait,
	}
}

func anchorKeys(anchors []Anchor) []string {
	keys := make([]string, 0, len(anchors))
	for _, anchor := range anchors {
		keys = append(keys, AnchorKey(anchor))
	}
	return keys
}
