package redact_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

func TestStringReturnsDeterministicMarkerWithEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		raw        string
		reason     string
		source     string
		wantReason string
		wantSource string
	}{
		{
			name:       "sensitive output",
			raw:        "super-secret",
			reason:     "sensitive_output",
			source:     "terraform_state_output.db_password",
			wantReason: "sensitive_output",
			wantSource: "terraform_state_output.db_password",
		},
		{
			name:       "empty value fails closed",
			raw:        "",
			reason:     "unknown_sensitive_value",
			source:     "terraform_state_attribute.token",
			wantReason: "unknown_sensitive_value",
			wantSource: "terraform_state_attribute.token",
		},
		{
			name:       "blank evidence normalizes",
			raw:        "secret",
			reason:     " ",
			source:     "",
			wantReason: "unknown",
			wantSource: "unknown",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			first := redact.String(test.raw, test.reason, test.source)
			second := redact.String(test.raw, test.reason, test.source)

			if first != second {
				t.Fatalf("String() = %#v, want deterministic %#v", second, first)
			}
			if first.Marker == "" {
				t.Fatalf("String().Marker is empty")
			}
			if strings.Contains(first.Marker, test.raw) && test.raw != "" {
				t.Fatalf("String().Marker = %q, leaked raw value", first.Marker)
			}
			if strings.Contains(first.Marker, test.wantSource) {
				t.Fatalf("String().Marker = %q, leaked source context", first.Marker)
			}
			if got := first.Reason; got != test.wantReason {
				t.Fatalf("String().Reason = %q, want %q", got, test.wantReason)
			}
			if got := first.Source; got != test.wantSource {
				t.Fatalf("String().Source = %q, want %q", got, test.wantSource)
			}
		})
	}
}

func TestRedactedMarkerDoesNotLeakRawValue(t *testing.T) {
	t.Parallel()

	redacted := redact.String("super-secret", "known_sensitive_key", "aws_db_instance.password")

	if strings.Contains(redacted.Marker, "super-secret") {
		t.Fatalf("String().Marker = %q, leaked raw value", redacted.Marker)
	}
	if got, wantPrefix := redacted.Marker, "redacted:sha256:"; !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("String().Marker = %q, want prefix %q", got, wantPrefix)
	}
}

func TestMarkerIncludesReasonAndSourceInDigest(t *testing.T) {
	t.Parallel()

	base := redact.String("same-secret", "sensitive_output", "terraform_state_output.api_token")
	otherReason := redact.String("same-secret", "known_sensitive_key", "terraform_state_output.api_token")
	otherSource := redact.String("same-secret", "sensitive_output", "terraform_state_output.db_password")

	if base.Marker == otherReason.Marker {
		t.Fatalf("String() marker did not change when reason changed: %q", base.Marker)
	}
	if base.Marker == otherSource.Marker {
		t.Fatalf("String() marker did not change when source changed: %q", base.Marker)
	}
}

func TestBytesAndScalarUseSameCanonicalBytes(t *testing.T) {
	t.Parallel()

	fromString := redact.String("42", "known_sensitive_key", "aws_instance.secret")
	fromBytes := redact.Bytes([]byte("42"), "known_sensitive_key", "aws_instance.secret")
	fromScalar := redact.Scalar(42, "known_sensitive_key", "aws_instance.secret")

	if fromString.Marker != fromBytes.Marker {
		t.Fatalf("Bytes().Marker = %q, want String().Marker %q", fromBytes.Marker, fromString.Marker)
	}
	if fromScalar.Marker != fromString.Marker {
		t.Fatalf("Scalar().Marker = %q, want String().Marker %q", fromScalar.Marker, fromString.Marker)
	}
}

func TestScalarDoesNotLeakUnsupportedValues(t *testing.T) {
	t.Parallel()

	raw := struct {
		Secret string
	}{Secret: "do-not-serialize"}

	redacted := redact.Scalar(raw, "unknown_sensitive_value", "unknown_provider_schema")

	if redacted.Marker == "" {
		t.Fatalf("Scalar() unsupported value returned empty marker")
	}
	if strings.Contains(redacted.Marker, raw.Secret) {
		t.Fatalf("Scalar().Marker = %q, leaked unsupported raw value", redacted.Marker)
	}
}
