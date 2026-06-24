// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status_test

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestDefaultAnswerNarrationStatusKeepsDeterministicFallback(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(status.RawSnapshot{}, status.DefaultOptions())
	narration := report.AnswerNarration

	if got, want := narration.State, status.AnswerNarrationUnavailable; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
	if got, want := narration.Reason, status.AnswerNarrationReasonDisabledByDefault; got != want {
		t.Fatalf("Reason = %q, want %q", got, want)
	}
	if narration.ProviderConfigured {
		t.Fatal("ProviderConfigured = true, want false")
	}
	if narration.PolicyAllowed {
		t.Fatal("PolicyAllowed = true, want false")
	}
	if !narration.DeterministicFallbackAvailable {
		t.Fatal("DeterministicFallbackAvailable = false, want true")
	}
	if narration.CanonicalTruthAffected {
		t.Fatal("CanonicalTruthAffected = true, want false")
	}
	if got, want := narration.RetentionPosture, status.AnswerNarrationRetentionMetadataOnly; got != want {
		t.Fatalf("RetentionPosture = %q, want %q", got, want)
	}
}

func TestAnswerNarrationSupportedReasonsIncludeGateOutcomes(t *testing.T) {
	t.Parallel()

	reasons := status.AnswerNarrationSupportedReasons()
	for _, want := range []string{
		status.AnswerNarrationReasonDisabledByDefault,
		status.AnswerNarrationReasonPolicyDenied,
		status.AnswerNarrationReasonBudgetExhausted,
		status.AnswerNarrationReasonUnsafeOutput,
		status.AnswerNarrationReasonTimeout,
		status.AnswerNarrationReasonProviderUnavailable,
	} {
		if !slices.Contains(reasons, want) {
			t.Fatalf("AnswerNarrationSupportedReasons() = %#v, want %q", reasons, want)
		}
	}
}
