// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// scriptedSemanticGenerationCheck answers freshness probes from a script so
// tests can change the answer between the pre-write and post-write probes.
// When the script runs out, the last answer repeats.
type scriptedSemanticGenerationCheck struct {
	current []bool
	errs    []error
	calls   int
}

func (s *scriptedSemanticGenerationCheck) check(
	_ context.Context,
	_, _ string,
) (bool, error) {
	index := s.calls
	if index >= len(s.current) {
		index = len(s.current) - 1
	}
	s.calls++
	if index < len(s.errs) && s.errs[index] != nil {
		return false, s.errs[index]
	}
	return s.current[index], nil
}

func semanticFreshnessTestIntent() reducercontract.Intent {
	return reducercontract.Intent{
		IntentID:     "intent-1",
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
		SourceSystem: "git",
		Domain:       reducercontract.DomainSemanticEntityMaterialization,
		Status:       reducercontract.IntentStatusClaimed,
		EnqueuedAt:   time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC),
		AvailableAt:  time.Date(2026, time.April, 14, 12, 0, 0, 0, time.UTC),
	}
}

func semanticFreshnessContentEnvelopes() []facts.Envelope {
	return []facts.Envelope{
		{
			FactKind: "content_entity",
			SourceRef: facts.Ref{
				SourceURI: "/repo/src/app.js",
			},
			Payload: map[string]any{
				"repo_id":       "repo-1",
				"entity_id":     "function-1",
				"relative_path": "src/app.js",
				"entity_type":   "Function",
				"entity_name":   "getTab",
				"language":      "javascript",
				"method_kind":   "getter",
			},
		},
	}
}

// TestSemanticHandlerSupersededGenerationSkipsWrite is the #8611 primary-fence
// RED: when the freshness check reports a superseded generation, Handle must
// exit superseded before the graph write, so a late superseded run can never
// retract repo-wide and upsert stale rows over the active generation's truth.
func TestSemanticHandlerSupersededGenerationSkipsWrite(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	loader := &fakeSemanticEntityFactLoader{envelopes: semanticFreshnessContentEnvelopes()}
	writer := &recordingSemanticEntityWriter{
		result: EntityWriteResult{CanonicalWrites: 1},
	}
	publisher := &recordingSemanticEntityPhasePublisher{}
	check := &scriptedSemanticGenerationCheck{current: []bool{false}}

	handler := Handler{
		FactLoader:      loader,
		Writer:          writer,
		GenerationCheck: check.check,
		PhasePublisher:  publisher,
	}

	result, err := handler.Handle(context.Background(), semanticFreshnessTestIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil for a superseded skip", err)
	}
	if result.Status != reducercontract.ResultStatusSuperseded {
		t.Errorf("Handle() status = %q, want superseded", result.Status)
	}
	if len(writer.writes) != 0 {
		t.Errorf("Handle() performed %d graph writes, want 0 for a superseded generation", len(writer.writes))
	}
	if len(publisher.calls) != 0 {
		t.Errorf("Handle() published %d phase batches, want 0 for a superseded generation", len(publisher.calls))
	}
	logText := logs.String()
	for _, want := range []string{
		`"msg":"semantic entity materialization skipped: generation superseded"`,
		`"scope_id":"scope-1"`,
		`"generation_id":"generation-1"`,
		`"phase":"pre_write"`,
	} {
		if !strings.Contains(logText, want) {
			t.Errorf("superseded skip log missing %s:\n%s", want, logText)
		}
	}
}

// TestSemanticHandlerCurrentGenerationWithUnmatchedUIDsStillFailsProjectionBug
// pins the #8611 still-current path: a fresh generation whose rows match no
// canonical node keeps the #8587 poison-pill failure, never a superseded exit.
func TestSemanticHandlerCurrentGenerationWithUnmatchedUIDsStillFailsProjectionBug(t *testing.T) {
	loader := &fakeSemanticEntityFactLoader{envelopes: semanticFreshnessContentEnvelopes()}
	writer := &recordingSemanticEntityWriter{
		result: EntityWriteResult{
			CanonicalWrites:      1,
			RowsAttemptedByLabel: map[string]int{"Function": 1},
			RowsMatchedByLabel:   map[string]int{"Function": 0},
			UnmatchedEntityIDs:   []string{"content-entity:sec-py-func"},
		},
	}
	check := &scriptedSemanticGenerationCheck{current: []bool{true, true}}

	handler := Handler{
		FactLoader:      loader,
		Writer:          writer,
		GenerationCheck: check.check,
	}

	_, err := handler.Handle(context.Background(), semanticFreshnessTestIntent())
	if err == nil {
		t.Fatalf("Handle() error = nil, want non-retryable identity-divergence failure for a current generation")
	}
	if !strings.Contains(err.Error(), "content-entity:sec-py-func") {
		t.Errorf("Handle() error = %q, must name the unmatched uid", err.Error())
	}
	if reducercontract.IsRetryable(err) {
		t.Errorf("Handle() error is retryable; an identity-divergent uid can never heal on retry")
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) {
		t.Fatalf("Handle() error %T carries no FailureClass()", err)
	}
	if classified.FailureClass() != "projection_bug" {
		t.Errorf("Handle() error FailureClass() = %q, want projection_bug", classified.FailureClass())
	}
	if check.calls != 2 {
		t.Errorf("freshness check calls = %d, want 2 (pre-write fence and post-write re-check)", check.calls)
	}
}

// TestSemanticHandlerSupersededBetweenWriteAndVerifyReturnsSuperseded covers
// the #8611 secondary fence: a generation superseded after its graph write
// must exit superseded with the write's CanonicalWrites instead of failing
// as a derivation bug on nodes the newer generation legitimately removed.
// The completion log and the unmatched-uid WARN still emit first, so the
// operator keeps proof of what landed.
func TestSemanticHandlerSupersededBetweenWriteAndVerifyReturnsSuperseded(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	loader := &fakeSemanticEntityFactLoader{envelopes: semanticFreshnessContentEnvelopes()}
	writer := &recordingSemanticEntityWriter{
		result: EntityWriteResult{
			CanonicalWrites:      1,
			RowsAttemptedByLabel: map[string]int{"Function": 1},
			RowsMatchedByLabel:   map[string]int{"Function": 0},
			UnmatchedEntityIDs:   []string{"content-entity:sec-py-func"},
		},
	}
	check := &scriptedSemanticGenerationCheck{current: []bool{true, false}}

	handler := Handler{
		FactLoader:      loader,
		Writer:          writer,
		GenerationCheck: check.check,
	}

	result, err := handler.Handle(context.Background(), semanticFreshnessTestIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil for a generation superseded after its write", err)
	}
	if result.Status != reducercontract.ResultStatusSuperseded {
		t.Errorf("Handle() status = %q, want superseded", result.Status)
	}
	if result.CanonicalWrites != 1 {
		t.Errorf("Handle() CanonicalWrites = %d, want the write result's 1", result.CanonicalWrites)
	}
	if len(writer.writes) != 1 {
		t.Fatalf("Handle() performed %d graph writes, want 1 (the fence only re-checks after the write)", len(writer.writes))
	}
	logText := logs.String()
	for _, want := range []string{
		`"msg":"semantic entity rows matched no canonical node"`,
		`"msg":"semantic entity materialization completed"`,
		`"msg":"semantic entity materialization skipped: generation superseded"`,
		`"phase":"post_write"`,
	} {
		if !strings.Contains(logText, want) {
			t.Errorf("logs missing %s:\n%s", want, logText)
		}
	}
}

// TestSemanticHandlerNotYetActiveGenerationDefersRetryable covers the #6686
// edge: a generation pending before first activation is not superseded. The
// check's GenerationNotYetActiveError must propagate wrapped so the queue
// retries the intent instead of acking a skip nothing re-drives.
func TestSemanticHandlerNotYetActiveGenerationDefersRetryable(t *testing.T) {
	loader := &fakeSemanticEntityFactLoader{envelopes: semanticFreshnessContentEnvelopes()}
	writer := &recordingSemanticEntityWriter{
		result: EntityWriteResult{CanonicalWrites: 1},
	}
	pending := reducercontract.GenerationNotYetActiveError{
		ScopeID:            "scope-1",
		GenerationID:       "generation-1",
		ActiveGenerationID: "generation-0",
	}
	check := &scriptedSemanticGenerationCheck{
		current: []bool{false},
		errs:    []error{pending},
	}

	handler := Handler{
		FactLoader:      loader,
		Writer:          writer,
		GenerationCheck: check.check,
	}

	_, err := handler.Handle(context.Background(), semanticFreshnessTestIntent())
	if err == nil {
		t.Fatalf("Handle() error = nil, want the wrapped not-yet-active deferral")
	}
	if !errors.Is(err, reducercontract.ErrGenerationNotYetActive) {
		t.Errorf("Handle() error = %v, must unwrap to ErrGenerationNotYetActive", err)
	}
	if !reducercontract.IsRetryable(err) {
		t.Errorf("Handle() error is not retryable; a pending generation must retry after activation")
	}
	if len(writer.writes) != 0 {
		t.Errorf("Handle() performed %d graph writes, want 0 when the fence errors", len(writer.writes))
	}
}

// TestSemanticHandlerFreshnessCheckErrorFailsClosed pins fail-closed lookup
// failures: a check error is never read as supersession, so a blind fence
// cannot ack-skip work the newer generation never did.
func TestSemanticHandlerFreshnessCheckErrorFailsClosed(t *testing.T) {
	loader := &fakeSemanticEntityFactLoader{envelopes: semanticFreshnessContentEnvelopes()}
	writer := &recordingSemanticEntityWriter{
		result: EntityWriteResult{CanonicalWrites: 1},
	}
	lookupErr := errors.New("generation activation lookup unavailable")
	check := &scriptedSemanticGenerationCheck{
		current: []bool{false},
		errs:    []error{lookupErr},
	}

	handler := Handler{
		FactLoader:      loader,
		Writer:          writer,
		GenerationCheck: check.check,
	}

	_, err := handler.Handle(context.Background(), semanticFreshnessTestIntent())
	if err == nil {
		t.Fatalf("Handle() error = nil, want the wrapped lookup failure")
	}
	if !errors.Is(err, lookupErr) {
		t.Errorf("Handle() error = %v, must wrap the lookup failure with %%w", err)
	}
	if len(writer.writes) != 0 {
		t.Errorf("Handle() performed %d graph writes, want 0 when the fence errors", len(writer.writes))
	}
}

// TestSemanticHandlerRetractOnlySupersededGenerationSkipsWrite covers the
// retract-only shape (repos touched, zero rows): the primary fence sits
// before the write, so a superseded retract-only run skips its repo-wide
// retract instead of wiping the active generation's nodes.
func TestSemanticHandlerRetractOnlySupersededGenerationSkipsWrite(t *testing.T) {
	loader := &fakeSemanticEntityFactLoader{
		envelopes: []facts.Envelope{
			{
				FactKind: "repository",
				Payload: map[string]any{
					"repo_id": "repo-1",
				},
			},
		},
	}
	writer := &recordingSemanticEntityWriter{
		result: EntityWriteResult{CanonicalWrites: 0},
	}
	check := &scriptedSemanticGenerationCheck{current: []bool{false}}

	handler := Handler{
		FactLoader:      loader,
		Writer:          writer,
		GenerationCheck: check.check,
	}

	result, err := handler.Handle(context.Background(), semanticFreshnessTestIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil for a superseded retract-only skip", err)
	}
	if result.Status != reducercontract.ResultStatusSuperseded {
		t.Errorf("Handle() status = %q, want superseded", result.Status)
	}
	if len(writer.writes) != 0 {
		t.Errorf("Handle() performed %d graph writes, want 0 for a superseded retract-only run", len(writer.writes))
	}
}

// TestSemanticHandlerNilGenerationCheckKeepsLegacyBehavior pins the nil-guard:
// without a wired check, unmatched uids keep the #8587 poison-pill failure,
// byte-for-byte the pre-#8611 behavior.
func TestSemanticHandlerNilGenerationCheckKeepsLegacyBehavior(t *testing.T) {
	loader := &fakeSemanticEntityFactLoader{envelopes: semanticFreshnessContentEnvelopes()}
	writer := &recordingSemanticEntityWriter{
		result: EntityWriteResult{
			CanonicalWrites:      1,
			RowsAttemptedByLabel: map[string]int{"Function": 1},
			RowsMatchedByLabel:   map[string]int{"Function": 0},
			UnmatchedEntityIDs:   []string{"content-entity:sec-py-func"},
		},
	}

	handler := Handler{
		FactLoader: loader,
		Writer:     writer,
	}

	_, err := handler.Handle(context.Background(), semanticFreshnessTestIntent())
	if err == nil {
		t.Fatalf("Handle() error = nil, want the legacy non-retryable identity-divergence failure")
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) {
		t.Fatalf("Handle() error %T carries no FailureClass()", err)
	}
	if classified.FailureClass() != "projection_bug" {
		t.Errorf("Handle() error FailureClass() = %q, want projection_bug", classified.FailureClass())
	}
}
