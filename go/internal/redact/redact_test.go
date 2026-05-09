package redact_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

func testKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("deployment-redaction-pepper"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	return key
}

func TestNewKeyRejectsBlankMaterial(t *testing.T) {
	t.Parallel()

	if _, err := redact.NewKey([]byte(" ")); err == nil {
		t.Fatal("NewKey() error = nil, want non-nil")
	}
}

func TestKeyIsZeroReportsMissingMaterial(t *testing.T) {
	t.Parallel()

	var empty redact.Key
	if !empty.IsZero() {
		t.Fatal("empty Key IsZero() = false, want true")
	}
	if testKey(t).IsZero() {
		t.Fatal("configured Key IsZero() = true, want false")
	}
}

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

			key := testKey(t)
			first := redact.String(test.raw, test.reason, test.source, key)
			second := redact.String(test.raw, test.reason, test.source, key)

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

	redacted := redact.String("super-secret", "known_sensitive_key", "aws_db_instance.password", testKey(t))

	if strings.Contains(redacted.Marker, "super-secret") {
		t.Fatalf("String().Marker = %q, leaked raw value", redacted.Marker)
	}
	if got, wantPrefix := redacted.Marker, "redacted:hmac-sha256:"; !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("String().Marker = %q, want prefix %q", got, wantPrefix)
	}
}

func TestMarkerIncludesReasonAndSourceInDigest(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	base := redact.String("same-secret", "sensitive_output", "terraform_state_output.api_token", key)
	otherReason := redact.String("same-secret", "known_sensitive_key", "terraform_state_output.api_token", key)
	otherSource := redact.String("same-secret", "sensitive_output", "terraform_state_output.db_password", key)

	if base.Marker == otherReason.Marker {
		t.Fatalf("String() marker did not change when reason changed: %q", base.Marker)
	}
	if base.Marker == otherSource.Marker {
		t.Fatalf("String() marker did not change when source changed: %q", base.Marker)
	}
}

func TestMarkerIncludesKeyInDigest(t *testing.T) {
	t.Parallel()

	firstKey, err := redact.NewKey([]byte("deployment-redaction-pepper-a"))
	if err != nil {
		t.Fatalf("NewKey(first) error = %v, want nil", err)
	}
	secondKey, err := redact.NewKey([]byte("deployment-redaction-pepper-b"))
	if err != nil {
		t.Fatalf("NewKey(second) error = %v, want nil", err)
	}

	first := redact.String("same-secret", "known_sensitive_key", "aws_instance.secret", firstKey)
	second := redact.String("same-secret", "known_sensitive_key", "aws_instance.secret", secondKey)

	if first.Marker == second.Marker {
		t.Fatalf("String() marker did not change when key changed: %q", first.Marker)
	}
}

func TestBytesAndScalarUseSameCanonicalBytes(t *testing.T) {
	t.Parallel()

	key := testKey(t)
	fromString := redact.String("42", "known_sensitive_key", "aws_instance.secret", key)
	fromBytes := redact.Bytes([]byte("42"), "known_sensitive_key", "aws_instance.secret", key)
	fromScalar := redact.Scalar(42, "known_sensitive_key", "aws_instance.secret", key)

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

	redacted := redact.Scalar(raw, "unknown_sensitive_value", "unknown_provider_schema", testKey(t))

	if redacted.Marker == "" {
		t.Fatalf("Scalar() unsupported value returned empty marker")
	}
	if strings.Contains(redacted.Marker, raw.Secret) {
		t.Fatalf("Scalar().Marker = %q, leaked unsupported raw value", redacted.Marker)
	}
}
