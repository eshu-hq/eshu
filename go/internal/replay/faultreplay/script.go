// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package faultreplay

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// CurrentVersion is the only fault-script schema version this package parses.
// Any other value is a hard parse error — there is no shimming or best-effort
// decode of an older or newer fault-script shape.
const CurrentVersion = 1

// Fault kind identifiers. These are the only five fault kinds a script may
// name; Script.Validate rejects any other kind. See design doc
// docs/internal/design/4389-ifa-conformance-platform.md, Layer 4, for the
// mechanism each kind targets.
const (
	// KindKillWorkerAfterClaim kills a reducer worker after it has claimed its
	// Nth work item, exercising lease-expiry reclaim.
	KindKillWorkerAfterClaim = "kill-worker-after-claim"
	// KindExpireLeaseMidHandler force-expires a claimed intent's lease while its
	// handler is still running, exercising the same reclaim path from the
	// opposite trigger (handler-side rather than worker-lifecycle-side).
	KindExpireLeaseMidHandler = "expire-lease-mid-handler"
	// KindFailGraphWriteOnceThenSucceed fails one graph write and then lets the
	// identical retried write succeed, exercising retry-with-backoff and
	// idempotent replay (MERGE / ON CONFLICT). Target.Lane says which retry path
	// is expected to observe the failure.
	KindFailGraphWriteOnceThenSucceed = "fail-graph-write-once-then-succeed"
	// KindRestartBackendBetweenPhaseGroups restarts the graph backend between
	// two scripted phase groups, exercising recovery across a backend outage.
	KindRestartBackendBetweenPhaseGroups = "restart-backend-between-phase-groups"
	// KindFailTerminal names an intent that must fail durably (never recover),
	// so a fault run can assert "dead letters appear only where the script
	// injected a terminal failure" is a non-vacuous check.
	KindFailTerminal = "fail-terminal"
)

// Lane identifiers for FaultOp.Target.Lane on
// fail-graph-write-once-then-succeed. The lane is load-bearing, not
// decorative, and it names WHERE the retry happens: executor-retry is retried
// in place by the reducer's RetryingExecutor (the intent never leaves the
// claim), while queue-retry surfaces to WorkSink.Fail and is re-queued as a
// retrying intent. Both lanes model a real transient graph write; a script
// that does not say which lane it expects cannot assert which recovery path
// ran. The hermetic runner (faultreplay) and the in-binary decorator
// (cypher.FaultingExecutor) realize the lanes differently -- see their type
// docs -- but a fault-free-identical drain with zero dead letters is the shared
// assertion.
const (
	// LaneExecutorRetry means the injected failure is classified transient and
	// retried in place by the executor decorator, without the intent ever
	// leaving the claim.
	LaneExecutorRetry = "executor-retry"
	// LaneQueueRetry means the injected failure surfaces to WorkSink.Fail and
	// the retry happens by the intent becoming claimable again through the
	// queue. The hermetic runner drives that re-queue explicitly (RedeliverOnce),
	// so its injected error may be a plain non-RetryableError; the in-binary
	// decorator relies on the real reducer queue, so its injected error is a
	// retryable graph_write_timeout error (the shape a real transient carries),
	// which is what makes WorkSink.Fail re-enqueue it rather than dead-letter.
	LaneQueueRetry = "queue-retry"
)

// Script is the root document of a fault-injection script (schema v1). A
// Script is pure data: it names faults to inject and the ordinal event (or
// stable ID) that fires each one. It carries no runner and no decorator
// wiring — those are separate slices that consume a validated Script.
type Script struct {
	// Version must equal CurrentVersion.
	Version int `json:"version"`
	// Faults is the ordered list of scripted fault operations. An empty list is
	// a valid fault-free baseline script.
	Faults []FaultOp `json:"faults,omitempty"`
}

// FaultOp is one scripted fault injection: what to break (Kind), when to fire
// it (Trigger), and, for fault kinds that need it, which recovery path is
// expected to observe it (Target).
type FaultOp struct {
	// Kind is one of the Kind* constants.
	Kind string `json:"kind"`
	// Trigger names the ordinal event, or stable ID, that fires this fault.
	Trigger Trigger `json:"trigger"`
	// Target carries fault-kind-specific effect parameters. Only
	// fail-graph-write-once-then-succeed uses it today (Target.Lane).
	Target Target `json:"target,omitzero"`
}

// Trigger names when a fault fires. Every populated field MUST be an ordinal
// over observed events (a claim count, an intent's position in the scripted
// delivery order, a phase-group count) or a stable string ID (an intent ID, a
// Cypher operation substring match) — never a duration, wall-clock timestamp,
// or random draw. A wall-clock or random trigger would make the fault fire at
// a different point on every run, so the fault run could no longer be
// replayed byte-for-byte and the platform's byte-identical canonical-graph
// assertion (design doc 4389, Layer 4) would be defeated. The AfterDuration,
// AtTimestamp, and RandomSeed fields exist only so Script.Validate has
// something concrete to reject; no fault kind ever legitimately sets them.
type Trigger struct {
	// AfterClaims is the 1-based count of claims after which
	// kill-worker-after-claim fires. Must be >= 1.
	AfterClaims *int `json:"after_claims,omitempty"`
	// IntentOrdinal is the 1-based position of the intent, in the scripted
	// delivery order, that expire-lease-mid-handler targets. Must be >= 1.
	// Mutually exclusive with IntentID.
	IntentOrdinal *int `json:"intent_ordinal,omitempty"`
	// IntentID is the stable intent ID that expire-lease-mid-handler (or
	// fail-terminal) targets. Mutually exclusive with IntentOrdinal on
	// expire-lease-mid-handler; the sole trigger field on fail-terminal.
	IntentID *string `json:"intent_id,omitempty"`
	// StatementOrdinal is the 1-based position of the graph-write statement
	// that fail-graph-write-once-then-succeed targets. Must be >= 1. Mutually
	// exclusive with OperationMatch.
	StatementOrdinal *int `json:"statement_ordinal,omitempty"`
	// OperationMatch is a substring match against the graph-write statement
	// text that fail-graph-write-once-then-succeed targets. Mutually exclusive
	// with StatementOrdinal.
	OperationMatch *string `json:"operation_match,omitempty"`
	// AfterPhaseGroups is the 1-based count of completed phase groups after
	// which restart-backend-between-phase-groups fires. Must be >= 1.
	AfterPhaseGroups *int `json:"after_phase_groups,omitempty"`

	// AfterDuration, if set, is always rejected by Validate: a duration-based
	// trigger is a wall-clock trigger under a different name.
	AfterDuration *string `json:"after_duration,omitempty"`
	// AtTimestamp, if set, is always rejected by Validate: a wall-clock trigger.
	AtTimestamp *string `json:"at_timestamp,omitempty"`
	// RandomSeed, if set, is always rejected by Validate: a random draw is the
	// opposite of a replayable ordinal trigger.
	RandomSeed *int64 `json:"random_seed,omitempty"`
}

// Target carries fault-kind-specific effect parameters.
type Target struct {
	// Lane is required on fail-graph-write-once-then-succeed and must be
	// LaneExecutorRetry or LaneQueueRetry. Unused (and must be left empty) by
	// every other fault kind.
	Lane string `json:"lane,omitempty"`
}

// Parse decodes fault-script JSON bytes into a validated Script. It rejects
// any JSON field the schema does not know about (via DisallowUnknownFields),
// then runs Script.Validate, so a Script Parse returns is always fail-closed:
// version 1, every fault a known kind with a well-formed trigger and target.
func Parse(data []byte) (Script, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var s Script
	if err := dec.Decode(&s); err != nil {
		return Script{}, fmt.Errorf("parse fault script: unknown field or malformed JSON: %w", err)
	}
	// json.Decoder.Decode reads only the first JSON value off the stream;
	// anything after it is silently ignored unless the stream is explicitly
	// checked for EOF. Without this check, a second script (or any other
	// trailing JSON) appended after a valid one would parse as if only the
	// first object existed -- fail closed instead of silently truncating.
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Script{}, errors.New("parse fault script: trailing content after fault script")
		}
		return Script{}, fmt.Errorf("parse fault script: malformed trailing content after fault script: %w", err)
	}
	if err := s.Validate(); err != nil {
		return Script{}, fmt.Errorf("invalid fault script: %w", err)
	}
	return s, nil
}

// Load reads a fault script from path and parses it with Parse.
func Load(path string) (Script, error) {
	// #nosec G304 -- path is an operator-supplied fault-script location (a
	// -fault-script flag or repo-shipped testdata fixture), not user- or
	// request-derived input.
	data, err := os.ReadFile(path)
	if err != nil {
		return Script{}, fmt.Errorf("read fault script %q: %w", path, err)
	}
	s, err := Parse(data)
	if err != nil {
		return Script{}, fmt.Errorf("fault script %q: %w", path, err)
	}
	return s, nil
}

// Validate checks the script's version and every fault op's shape. It is the
// single fail-closed gate this package provides: a Script that passes
// Validate has a supported version, only known fault kinds, well-formed
// mutually-exclusive trigger pairs, only positive ordinals, and no
// non-ordinal (duration/timestamp/random) trigger field.
func (s Script) Validate() error {
	if s.Version != CurrentVersion {
		return fmt.Errorf("unsupported version %d (want %d)", s.Version, CurrentVersion)
	}
	for i, f := range s.Faults {
		if err := f.validate(); err != nil {
			return fmt.Errorf("faults[%d]: %w", i, err)
		}
	}
	return nil
}

// allowedTriggerFields maps each fault kind to the trigger field name(s) it is
// allowed to populate. validate rejects a populated field outside this set
// even when the field is a legitimate name for a DIFFERENT kind (e.g.
// kill-worker-after-claim setting statement_ordinal, which belongs to
// fail-graph-write-once-then-succeed): DisallowUnknownFields only catches an
// unrecognized JSON key, not a known key used on the wrong fault kind.
var allowedTriggerFields = map[string]map[string]bool{
	KindKillWorkerAfterClaim:             {"after_claims": true},
	KindExpireLeaseMidHandler:            {"intent_ordinal": true, "intent_id": true},
	KindFailGraphWriteOnceThenSucceed:    {"statement_ordinal": true, "operation_match": true},
	KindRestartBackendBetweenPhaseGroups: {"after_phase_groups": true},
	KindFailTerminal:                     {"intent_id": true},
}

// validate checks one fault op's kind, trigger, and target.
func (f FaultOp) validate() error {
	if err := f.Trigger.validateOrdinalOnly(); err != nil {
		return err
	}
	allowed, ok := allowedTriggerFields[f.Kind]
	if !ok {
		return fmt.Errorf("unknown fault kind %q", f.Kind)
	}
	if err := f.Trigger.rejectFieldsOutsideKind(f.Kind, allowed); err != nil {
		return err
	}
	switch f.Kind {
	case KindKillWorkerAfterClaim:
		return f.validateKillWorkerAfterClaim()
	case KindExpireLeaseMidHandler:
		return f.validateExpireLeaseMidHandler()
	case KindFailGraphWriteOnceThenSucceed:
		return f.validateFailGraphWriteOnceThenSucceed()
	case KindRestartBackendBetweenPhaseGroups:
		return f.validateRestartBackendBetweenPhaseGroups()
	case KindFailTerminal:
		return f.validateFailTerminal()
	default:
		return fmt.Errorf("unknown fault kind %q", f.Kind)
	}
}

// populatedOrdinalFields lists, by JSON field name, every ordinal/ID trigger
// field this Trigger has set. Used both to detect a cross-kind field (a known
// field name that does not belong to this FaultOp's kind) and to build a
// clear error naming exactly which field is out of place.
func (t Trigger) populatedOrdinalFields() []string {
	var out []string
	if t.AfterClaims != nil {
		out = append(out, "after_claims")
	}
	if t.IntentOrdinal != nil {
		out = append(out, "intent_ordinal")
	}
	if t.IntentID != nil {
		out = append(out, "intent_id")
	}
	if t.StatementOrdinal != nil {
		out = append(out, "statement_ordinal")
	}
	if t.OperationMatch != nil {
		out = append(out, "operation_match")
	}
	if t.AfterPhaseGroups != nil {
		out = append(out, "after_phase_groups")
	}
	return out
}

// rejectFieldsOutsideKind returns an error naming every populated trigger
// field kind does not accept. A field with a legitimate name for a DIFFERENT
// kind (e.g. kill-worker-after-claim setting statement_ordinal) is exactly
// the class DisallowUnknownFields cannot catch, since the field name is
// real -- just wrong for this kind.
func (t Trigger) rejectFieldsOutsideKind(kind string, allowed map[string]bool) error {
	var bad []string
	for _, name := range t.populatedOrdinalFields() {
		if !allowed[name] {
			bad = append(bad, name)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("kind %q does not accept trigger field(s) %s", kind, strings.Join(bad, ", "))
}

// validateOrdinalOnly rejects any populated non-ordinal trigger field. See the
// Trigger doc comment for why: a wall-clock or random trigger would make the
// fault run non-replayable, defeating the byte-identical canonical-graph
// assertion the fault-injection gate exists to make.
func (t Trigger) validateOrdinalOnly() error {
	var bad []string
	if t.AfterDuration != nil {
		bad = append(bad, "after_duration")
	}
	if t.AtTimestamp != nil {
		bad = append(bad, "at_timestamp")
	}
	if t.RandomSeed != nil {
		bad = append(bad, "random_seed")
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf(
		"trigger carries non-ordinal field(s) %s: a fault trigger must be an "+
			"ordinal over observed events or a stable ID, never a wall-clock, "+
			"duration, or random-draw field",
		strings.Join(bad, ", "),
	)
}

func (f FaultOp) validateKillWorkerAfterClaim() error {
	if f.Trigger.AfterClaims == nil {
		return errors.New("kill-worker-after-claim requires trigger.after_claims")
	}
	if *f.Trigger.AfterClaims < 1 {
		return fmt.Errorf("trigger.after_claims must be >= 1, got %d", *f.Trigger.AfterClaims)
	}
	return nil
}

func (f FaultOp) validateExpireLeaseMidHandler() error {
	if err := exactlyOne(f.Trigger.IntentOrdinal != nil, f.Trigger.IntentID != nil,
		"trigger.intent_ordinal", "trigger.intent_id"); err != nil {
		return err
	}
	if f.Trigger.IntentOrdinal != nil && *f.Trigger.IntentOrdinal < 1 {
		return fmt.Errorf("trigger.intent_ordinal must be >= 1, got %d", *f.Trigger.IntentOrdinal)
	}
	if f.Trigger.IntentID != nil && strings.TrimSpace(*f.Trigger.IntentID) == "" {
		return errors.New("trigger.intent_id must not be empty")
	}
	return nil
}

func (f FaultOp) validateFailGraphWriteOnceThenSucceed() error {
	if err := exactlyOne(f.Trigger.StatementOrdinal != nil, f.Trigger.OperationMatch != nil,
		"trigger.statement_ordinal", "trigger.operation_match"); err != nil {
		return err
	}
	if f.Trigger.StatementOrdinal != nil && *f.Trigger.StatementOrdinal < 1 {
		return fmt.Errorf("trigger.statement_ordinal must be >= 1, got %d", *f.Trigger.StatementOrdinal)
	}
	if f.Trigger.OperationMatch != nil && strings.TrimSpace(*f.Trigger.OperationMatch) == "" {
		return errors.New("trigger.operation_match must not be empty")
	}
	switch f.Target.Lane {
	case LaneExecutorRetry, LaneQueueRetry:
		return nil
	case "":
		return errors.New("fail-graph-write-once-then-succeed requires target.lane")
	default:
		return fmt.Errorf("unknown target.lane %q (want %q or %q)", f.Target.Lane, LaneExecutorRetry, LaneQueueRetry)
	}
}

func (f FaultOp) validateRestartBackendBetweenPhaseGroups() error {
	if f.Trigger.AfterPhaseGroups == nil {
		return errors.New("restart-backend-between-phase-groups requires trigger.after_phase_groups")
	}
	if *f.Trigger.AfterPhaseGroups < 1 {
		return fmt.Errorf("trigger.after_phase_groups must be >= 1, got %d", *f.Trigger.AfterPhaseGroups)
	}
	return nil
}

func (f FaultOp) validateFailTerminal() error {
	if f.Trigger.IntentID == nil || strings.TrimSpace(*f.Trigger.IntentID) == "" {
		return errors.New("fail-terminal requires trigger.intent_id")
	}
	return nil
}

// exactlyOne returns an error unless exactly one of a, b is true — the
// mutually-exclusive trigger-field discriminant shared by
// expire-lease-mid-handler (intent_ordinal vs intent_id) and
// fail-graph-write-once-then-succeed (statement_ordinal vs operation_match).
func exactlyOne(a, b bool, aName, bName string) error {
	switch {
	case a && b:
		return fmt.Errorf("trigger must set exactly one of %s or %s, not both", aName, bName)
	case !a && !b:
		return fmt.Errorf("trigger must set exactly one of %s or %s", aName, bName)
	default:
		return nil
	}
}
