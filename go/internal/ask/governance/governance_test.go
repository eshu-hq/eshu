package governance_test

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/ask/governance"
	"github.com/eshu-hq/eshu/go/internal/status"
)

// allTrue returns a PostureInputs with every gate open.
func allTrue() governance.PostureInputs {
	return governance.PostureInputs{
		ProviderConfigured:     true,
		ProviderTrafficEnabled: true,
		PolicyAllowed:          true,
		BudgetAvailable:        true,
		PublishSafetyEnabled:   true,
	}
}

var fixedNow = time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)

func resolveAt(in governance.PostureInputs) status.AnswerNarrationStatus {
	return governance.ResolvePosture(in, fixedNow)
}

// TestResolvePosture_DefaultClosed verifies that the zero-value PostureInputs
// (all gates false) yields Unavailable, not Available.
func TestResolvePosture_DefaultClosed(t *testing.T) {
	t.Parallel()

	got := resolveAt(governance.PostureInputs{})

	if got.State == status.AnswerNarrationAvailable {
		t.Errorf("default-closed: expected non-Available, got State=%q", got.State)
	}
}

// TestResolvePosture_AllTrue verifies that opening every gate yields Available.
func TestResolvePosture_AllTrue(t *testing.T) {
	t.Parallel()

	got := resolveAt(allTrue())

	if got.State != status.AnswerNarrationAvailable {
		t.Errorf("all-true: want State=%q got %q (Reason=%q)", status.AnswerNarrationAvailable, got.State, got.Reason)
	}
	if got.Reason != status.AnswerNarrationReasonAvailable {
		t.Errorf("all-true: want Reason=%q got %q", status.AnswerNarrationReasonAvailable, got.Reason)
	}
}

// TestResolvePosture_ProviderNotConfigured verifies ProviderUnavailable reason
// when the provider is not configured.
func TestResolvePosture_ProviderNotConfigured(t *testing.T) {
	t.Parallel()

	in := allTrue()
	in.ProviderConfigured = false

	got := resolveAt(in)

	if got.State == status.AnswerNarrationAvailable {
		t.Errorf("provider not configured: expected non-Available, got State=%q", got.State)
	}
	if got.Reason != status.AnswerNarrationReasonProviderUnavailable {
		t.Errorf("provider not configured: want Reason=%q got %q",
			status.AnswerNarrationReasonProviderUnavailable, got.Reason)
	}
}

// TestResolvePosture_ProviderTrafficDisabled verifies ProviderUnavailable reason
// when provider traffic is disabled.
func TestResolvePosture_ProviderTrafficDisabled(t *testing.T) {
	t.Parallel()

	in := allTrue()
	in.ProviderTrafficEnabled = false

	got := resolveAt(in)

	if got.State == status.AnswerNarrationAvailable {
		t.Errorf("traffic disabled: expected non-Available, got State=%q", got.State)
	}
	if got.Reason != status.AnswerNarrationReasonProviderUnavailable {
		t.Errorf("traffic disabled: want Reason=%q got %q",
			status.AnswerNarrationReasonProviderUnavailable, got.Reason)
	}
}

// TestResolvePosture_PolicyDenied verifies PolicyDenied reason when policy
// gate is closed.
func TestResolvePosture_PolicyDenied(t *testing.T) {
	t.Parallel()

	in := allTrue()
	in.PolicyAllowed = false

	got := resolveAt(in)

	if got.State == status.AnswerNarrationAvailable {
		t.Errorf("policy denied: expected non-Available, got State=%q", got.State)
	}
	if got.Reason != status.AnswerNarrationReasonPolicyDenied {
		t.Errorf("policy denied: want Reason=%q got %q",
			status.AnswerNarrationReasonPolicyDenied, got.Reason)
	}
}

// TestResolvePosture_BudgetExhausted verifies BudgetExhausted reason when
// budget gate is closed.
func TestResolvePosture_BudgetExhausted(t *testing.T) {
	t.Parallel()

	in := allTrue()
	in.BudgetAvailable = false

	got := resolveAt(in)

	if got.State == status.AnswerNarrationAvailable {
		t.Errorf("budget exhausted: expected non-Available, got State=%q", got.State)
	}
	if got.Reason != status.AnswerNarrationReasonBudgetExhausted {
		t.Errorf("budget exhausted: want Reason=%q got %q",
			status.AnswerNarrationReasonBudgetExhausted, got.Reason)
	}
}

// TestResolvePosture_PublishSafetyDisabled verifies DisabledByDefault reason
// (the catch-all) when publish safety is the only closed gate.
func TestResolvePosture_PublishSafetyDisabled(t *testing.T) {
	t.Parallel()

	in := allTrue()
	in.PublishSafetyEnabled = false

	got := resolveAt(in)

	if got.State == status.AnswerNarrationAvailable {
		t.Errorf("publish safety disabled: expected non-Available, got State=%q", got.State)
	}
	if got.Reason != status.AnswerNarrationReasonDisabledByDefault {
		t.Errorf("publish safety disabled: want Reason=%q got %q",
			status.AnswerNarrationReasonDisabledByDefault, got.Reason)
	}
}

// TestResolvePosture_DeterministicFallbackAlwaysTrue verifies that
// DeterministicFallbackAvailable is always true regardless of posture.
func TestResolvePosture_DeterministicFallbackAlwaysTrue(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   governance.PostureInputs
	}{
		{"all-false", governance.PostureInputs{}},
		{"all-true", allTrue()},
		{"no-provider", func() governance.PostureInputs { i := allTrue(); i.ProviderConfigured = false; return i }()},
		{"policy-denied", func() governance.PostureInputs { i := allTrue(); i.PolicyAllowed = false; return i }()},
		{"budget-exhausted", func() governance.PostureInputs { i := allTrue(); i.BudgetAvailable = false; return i }()},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveAt(tc.in)
			if !got.DeterministicFallbackAvailable {
				t.Errorf("%s: DeterministicFallbackAvailable must always be true", tc.name)
			}
		})
	}
}

// TestResolvePosture_CanonicalTruthNeverAffected verifies that
// CanonicalTruthAffected is always false.
func TestResolvePosture_CanonicalTruthNeverAffected(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   governance.PostureInputs
	}{
		{"all-false", governance.PostureInputs{}},
		{"all-true", allTrue()},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveAt(tc.in)
			if got.CanonicalTruthAffected {
				t.Errorf("%s: CanonicalTruthAffected must always be false", tc.name)
			}
		})
	}
}

// TestResolvePosture_RetentionPosture verifies metadata-only retention posture.
func TestResolvePosture_RetentionPosture(t *testing.T) {
	t.Parallel()

	cases := []governance.PostureInputs{
		{},
		allTrue(),
	}

	for _, in := range cases {
		got := resolveAt(in)
		if got.RetentionPosture != status.AnswerNarrationRetentionMetadataOnly {
			t.Errorf("RetentionPosture: want %q got %q",
				status.AnswerNarrationRetentionMetadataOnly, got.RetentionPosture)
		}
	}
}

// TestResolvePosture_UpdatedAt verifies the UpdatedAt stamp matches the injected now.
func TestResolvePosture_UpdatedAt(t *testing.T) {
	t.Parallel()

	got := resolveAt(allTrue())
	if !got.UpdatedAt.Equal(fixedNow) {
		t.Errorf("UpdatedAt: want %v got %v", fixedNow, got.UpdatedAt)
	}
}

// TestAskOutcomeValid verifies the bounded AskOutcome validity predicate.
func TestAskOutcomeValid(t *testing.T) {
	t.Parallel()

	known := []governance.AskOutcome{
		governance.AskAnswered,
		governance.AskPartial,
		governance.AskNarrated,
		governance.AskDeterministic,
		governance.AskDenied,
		governance.AskError,
	}

	for _, o := range known {
		o := o
		t.Run(string(o), func(t *testing.T) {
			t.Parallel()
			if !o.Valid() {
				t.Errorf("AskOutcome %q: Valid() returned false for known outcome", o)
			}
		})
	}

	unknown := governance.AskOutcome("totally_unknown")
	if unknown.Valid() {
		t.Errorf("AskOutcome %q: Valid() returned true for unknown outcome", unknown)
	}
}

// TestAskStageValid verifies the bounded AskStage validity predicate.
func TestAskStageValid(t *testing.T) {
	t.Parallel()

	known := []governance.AskStage{
		governance.AskStagePlan,
		governance.AskStageTool,
		governance.AskStageNarrate,
		governance.AskStageRender,
	}

	for _, s := range known {
		s := s
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()
			if !s.Valid() {
				t.Errorf("AskStage %q: Valid() returned false for known stage", s)
			}
		})
	}

	unknown := governance.AskStage("totally_unknown")
	if unknown.Valid() {
		t.Errorf("AskStage %q: Valid() returned true for unknown stage", unknown)
	}
}
