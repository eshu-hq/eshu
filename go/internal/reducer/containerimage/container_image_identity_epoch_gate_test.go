// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"errors"
	"strings"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// epochGateScriptWriter replays one epoch read result per call so the gate
// matrix can script a miss followed by a re-read hit or miss.
type epochGateScriptWriter struct {
	epochs []int64
	errs   []error
	calls  int
}

func (w *epochGateScriptWriter) ContainerImageIdentityActivationEpoch(
	_ context.Context,
	_, _ string,
) (int64, error) {
	i := w.calls
	w.calls++
	if i >= len(w.errs) {
		return 0, errors.New("unexpected container image identity epoch read")
	}
	return w.epochs[i], w.errs[i]
}

func (w *epochGateScriptWriter) WriteContainerImageIdentityDecisions(
	_ context.Context,
	_ ContainerImageIdentityWrite,
) (ContainerImageIdentityWriteResult, error) {
	return ContainerImageIdentityWriteResult{}, nil
}

// TestGatedActivationEpochClassifiesMiss pins the #6502 gate matrix: a hit
// proceeds, a non-sentinel failure stays loud without consulting the check,
// and a sentinel miss defers, supersedes, or re-reads by the check's verdict.
func TestGatedActivationEpochClassifiesMiss(t *testing.T) {
	sentinel := reducercontract.ErrContainerImageIdentityGenerationNotActive
	deferErr := reducercontract.GenerationNotYetActiveError{
		ScopeID: "scope:6502", GenerationID: "generation:6502", ActiveGenerationID: "generation:6502-prior",
	}
	lookupErr := errors.New("synthetic generation lookup failure")
	otherErr := errors.New("synthetic epoch query failure")
	intent := reducercontract.Intent{
		IntentID:     "intent:6502",
		ScopeID:      "scope:6502",
		GenerationID: "generation:6502",
		Domain:       reducercontract.DomainContainerImageIdentity,
	}

	for _, test := range []struct {
		name string
		// epochs/errs script the writer; check scripts the freshness
		// verdict, with checkCalls counting consultations.
		epochs []int64
		errs   []error
		check  reducercontract.GenerationFreshnessCheck
		// wantEpoch/wantCalls pin the read path; wantResult selects the
		// superseded early result; wantAs/wantIs/wantText pin the error.
		wantEpoch  int64
		wantCalls  int
		wantChecks int
		wantResult bool
		wantAs     any
		wantIs     error
		wantText   string
	}{
		{
			name: "hit proceeds", epochs: []int64{7}, errs: []error{nil},
			check:      nil,
			wantEpoch:  7,
			wantCalls:  1,
			wantChecks: 0,
		},
		{
			name:   "non-sentinel failure stays loud",
			epochs: []int64{0}, errs: []error{otherErr},
			check: func(_ context.Context, _, _ string) (bool, error) {
				t.Error("check consulted for a non-sentinel failure")
				return false, nil
			},
			wantCalls:  1,
			wantChecks: 0,
			wantIs:     otherErr,
			wantText:   "read container image identity activation epoch",
		},
		{
			name:   "sentinel without check keeps the legacy loud error",
			epochs: []int64{0}, errs: []error{sentinel},
			check:      nil,
			wantCalls:  1,
			wantChecks: 0,
			wantIs:     sentinel,
			wantText:   "read container image identity activation epoch",
		},
		{
			name:   "pending generation defers",
			epochs: []int64{0}, errs: []error{sentinel},
			check: func(_ context.Context, _, _ string) (bool, error) {
				return false, deferErr
			},
			wantCalls:  1,
			wantChecks: 1,
			wantAs:     &reducercontract.GenerationNotYetActiveError{},
			wantText:   "check container image identity generation",
		},
		{
			name:   "superseded generation returns an early result",
			epochs: []int64{0}, errs: []error{sentinel},
			check: func(_ context.Context, _, _ string) (bool, error) {
				return false, nil
			},
			wantCalls:  1,
			wantChecks: 1,
			wantResult: true,
		},
		{
			name:   "current generation re-reads and proceeds",
			epochs: []int64{0, 9}, errs: []error{sentinel, nil},
			check: func(_ context.Context, _, _ string) (bool, error) {
				return true, nil
			},
			wantEpoch:  9,
			wantCalls:  2,
			wantChecks: 1,
		},
		{
			name:   "current generation with a still-missing epoch surfaces loudly",
			epochs: []int64{0, 0}, errs: []error{sentinel, sentinel},
			check: func(_ context.Context, _, _ string) (bool, error) {
				return true, nil
			},
			wantCalls:  2,
			wantChecks: 1,
			wantIs:     sentinel,
			wantText:   "read container image identity activation epoch",
		},
		{
			name:   "check lookup failure surfaces loudly",
			epochs: []int64{0}, errs: []error{sentinel},
			check: func(_ context.Context, _, _ string) (bool, error) {
				return false, lookupErr
			},
			wantCalls:  1,
			wantChecks: 1,
			wantIs:     lookupErr,
			wantText:   "check container image identity generation",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			writer := &epochGateScriptWriter{epochs: test.epochs, errs: test.errs}
			checks := 0
			check := test.check
			if check != nil {
				inner := check
				check = func(ctx context.Context, scopeID, generationID string) (bool, error) {
					checks++
					return inner(ctx, scopeID, generationID)
				}
			}
			handler := ContainerImageIdentityHandler{Writer: writer, GenerationCheck: check}
			epoch, result, err := handler.gatedActivationEpoch(context.Background(), intent)
			if writer.calls != test.wantCalls {
				t.Errorf("epoch reads = %d, want %d", writer.calls, test.wantCalls)
			}
			if checks != test.wantChecks {
				t.Errorf("check consultations = %d, want %d", checks, test.wantChecks)
			}
			if test.wantResult {
				if err != nil {
					t.Fatalf("gatedActivationEpoch() error = %v, want a superseded result", err)
				}
				if result == nil {
					t.Fatalf("gatedActivationEpoch() result = nil, want superseded")
				}
				if result.Status != reducercontract.ResultStatusSuperseded {
					t.Errorf("result status = %s, want superseded", result.Status)
				}
				if result.IntentID != intent.IntentID || result.Domain != intent.Domain {
					t.Errorf(
						"result identity = (%s, %s), want (%s, %s)",
						result.IntentID, result.Domain, intent.IntentID, intent.Domain,
					)
				}
				if result.CanonicalWrites != 0 {
					t.Errorf("result canonical writes = %d, want 0", result.CanonicalWrites)
				}
				if result.CompletedAt.IsZero() {
					t.Errorf("result completed_at is zero")
				}
				if !strings.Contains(result.EvidenceSummary, intent.GenerationID) ||
					!strings.Contains(result.EvidenceSummary, intent.ScopeID) {
					t.Errorf("result summary = %q, want scope and generation", result.EvidenceSummary)
				}
				return
			}
			if result != nil {
				t.Errorf("gatedActivationEpoch() result = %+v, want nil", *result)
			}
			if test.wantAs == nil && test.wantIs == nil {
				if err != nil {
					t.Fatalf("gatedActivationEpoch() error = %v, want nil", err)
				}
				if epoch != test.wantEpoch {
					t.Errorf("epoch = %d, want %d", epoch, test.wantEpoch)
				}
				return
			}
			if err == nil {
				t.Fatalf("gatedActivationEpoch() error = nil, want %q", test.wantText)
			}
			if test.wantAs != nil && !errors.As(err, test.wantAs) {
				t.Errorf("gatedActivationEpoch() error = %v, want %T", err, test.wantAs)
			}
			if test.wantIs != nil && !errors.Is(err, test.wantIs) {
				t.Errorf("gatedActivationEpoch() error = %v, want %v", err, test.wantIs)
			}
			if !strings.Contains(err.Error(), test.wantText) {
				t.Errorf("gatedActivationEpoch() error = %q, want text %q", err.Error(), test.wantText)
			}
		})
	}
}
