package redact_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

func TestRuleSetClassifiesKnownSensitiveKeys(t *testing.T) {
	t.Parallel()

	rules, err := redact.NewRuleSet("fixture-schema-2026-05-09", []string{
		"api_token",
		"api_keys",
		"password",
		"secret_access_key",
		"client-secret",
	})
	if err != nil {
		t.Fatalf("NewRuleSet() error = %v, want nil", err)
	}

	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "snake case key",
			source: "aws_iam_access_key.secret_access_key",
		},
		{
			name:   "camel case key normalizes",
			source: "aws_iam_access_key.SecretAccessKey",
		},
		{
			name:   "hyphenated rule normalizes",
			source: "aws_cognito_app_client.ClientSecret",
		},
		{
			name:   "acronym key normalizes",
			source: "custom_provider_resource.APIToken",
		},
		{
			name:   "indexed key matches parent field",
			source: "custom_provider_resource.api_keys[0]",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			decision := rules.Classify(test.source, redact.SchemaKnown, redact.FieldScalar)

			if got, want := decision.Action, redact.ActionRedact; got != want {
				t.Fatalf("Classify().Action = %q, want %q", got, want)
			}
			if got, want := decision.Reason, redact.ReasonKnownSensitiveKey; got != want {
				t.Fatalf("Classify().Reason = %q, want %q", got, want)
			}
			if got := decision.Source; got != test.source {
				t.Fatalf("Classify().Source = %q, want %q", got, test.source)
			}
			if got, want := decision.RuleSetVersion, "fixture-schema-2026-05-09"; got != want {
				t.Fatalf("Classify().RuleSetVersion = %q, want %q", got, want)
			}
		})
	}
}

func TestRuleSetPreservesKnownNonSensitiveKeys(t *testing.T) {
	t.Parallel()

	rules, err := redact.NewRuleSet("fixture-schema-2026-05-09", []string{"password"})
	if err != nil {
		t.Fatalf("NewRuleSet() error = %v, want nil", err)
	}

	decision := rules.Classify("fixture_resource.identifier", redact.SchemaKnown, redact.FieldScalar)

	if got, want := decision.Action, redact.ActionPreserve; got != want {
		t.Fatalf("Classify().Action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, redact.ReasonKnownProviderSchema; got != want {
		t.Fatalf("Classify().Reason = %q, want %q", got, want)
	}
	if got, want := decision.RuleSetVersion, "fixture-schema-2026-05-09"; got != want {
		t.Fatalf("Classify().RuleSetVersion = %q, want %q", got, want)
	}
}

func TestRuleSetDropsKnownSensitiveCompositeValues(t *testing.T) {
	t.Parallel()

	rules, err := redact.NewRuleSet("fixture-schema-2026-05-09", []string{"credentials"})
	if err != nil {
		t.Fatalf("NewRuleSet() error = %v, want nil", err)
	}

	decision := rules.Classify("fixture_provider.credentials", redact.SchemaKnown, redact.FieldComposite)

	if got, want := decision.Action, redact.ActionDrop; got != want {
		t.Fatalf("Classify().Action = %q, want %q", got, want)
	}
	if got, want := decision.Reason, redact.ReasonKnownSensitiveKey; got != want {
		t.Fatalf("Classify().Reason = %q, want %q", got, want)
	}
}

func TestRuleSetFailsClosedForUnknownProviderSchema(t *testing.T) {
	t.Parallel()

	rules, err := redact.NewRuleSet("fixture-schema-2026-05-09", []string{"password"})
	if err != nil {
		t.Fatalf("NewRuleSet() error = %v, want nil", err)
	}

	scalar := rules.Classify("custom_provider_resource.api_token", redact.SchemaUnknown, redact.FieldScalar)
	if got, want := scalar.Action, redact.ActionRedact; got != want {
		t.Fatalf("Classify(scalar).Action = %q, want %q", got, want)
	}
	if got, want := scalar.Reason, redact.ReasonUnknownProviderSchema; got != want {
		t.Fatalf("Classify(scalar).Reason = %q, want %q", got, want)
	}

	composite := rules.Classify("custom_provider_resource.settings", redact.SchemaUnknown, redact.FieldComposite)
	if got, want := composite.Action, redact.ActionDrop; got != want {
		t.Fatalf("Classify(composite).Action = %q, want %q", got, want)
	}
	if got, want := composite.Reason, redact.ReasonUnknownProviderSchema; got != want {
		t.Fatalf("Classify(composite).Reason = %q, want %q", got, want)
	}
}

func TestRuleSetFailsClosedWhenUninitialized(t *testing.T) {
	t.Parallel()

	var rules redact.RuleSet

	scalar := rules.Classify("fixture_resource.password", redact.SchemaKnown, redact.FieldScalar)
	if got, want := scalar.Action, redact.ActionRedact; got != want {
		t.Fatalf("Classify(scalar).Action = %q, want %q", got, want)
	}
	if got, want := scalar.Reason, redact.ReasonUnknownRuleSet; got != want {
		t.Fatalf("Classify(scalar).Reason = %q, want %q", got, want)
	}
	if got, want := scalar.RuleSetVersion, "unknown"; got != want {
		t.Fatalf("Classify(scalar).RuleSetVersion = %q, want %q", got, want)
	}

	composite := rules.Classify("fixture_resource.credentials", redact.SchemaKnown, redact.FieldComposite)
	if got, want := composite.Action, redact.ActionDrop; got != want {
		t.Fatalf("Classify(composite).Action = %q, want %q", got, want)
	}
	if got, want := composite.Reason, redact.ReasonUnknownRuleSet; got != want {
		t.Fatalf("Classify(composite).Reason = %q, want %q", got, want)
	}
}

func TestRuleSetFailsClosedForUnknownFieldKind(t *testing.T) {
	t.Parallel()

	rules, err := redact.NewRuleSet("fixture-schema-2026-05-09", []string{"password"})
	if err != nil {
		t.Fatalf("NewRuleSet() error = %v, want nil", err)
	}

	tests := []struct {
		name      string
		fieldKind redact.FieldKind
	}{
		{
			name:      "zero value",
			fieldKind: redact.FieldKind(""),
		},
		{
			name:      "invalid value",
			fieldKind: redact.FieldKind("object"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			decision := rules.Classify("fixture_resource.identifier", redact.SchemaKnown, test.fieldKind)

			if got, want := decision.Action, redact.ActionDrop; got != want {
				t.Fatalf("Classify().Action = %q, want %q", got, want)
			}
			if got, want := decision.Reason, redact.ReasonUnknownFieldKind; got != want {
				t.Fatalf("Classify().Reason = %q, want %q", got, want)
			}
		})
	}
}

func TestRuleSetRejectsBlankVersion(t *testing.T) {
	t.Parallel()

	if _, err := redact.NewRuleSet(" ", []string{"password"}); err == nil {
		t.Fatal("NewRuleSet() error = nil, want non-nil")
	}
}
