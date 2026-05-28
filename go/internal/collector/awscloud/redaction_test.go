package awscloud

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

func TestAWSRedactStringUsesVersionedLaunchPolicy(t *testing.T) {
	t.Parallel()

	key, err := redact.NewKey([]byte("aws-redaction-policy-test-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}

	value := RedactString(
		"postgres://user:password@example.internal/app",
		"lambda.environment.DATABASE_URL",
		key,
	)

	if got := value["ruleset_version"]; got != RedactionPolicyVersion {
		t.Fatalf("ruleset_version = %#v, want %q", got, RedactionPolicyVersion)
	}
	if got := value["reason"]; got != redact.ReasonKnownSensitiveKey {
		t.Fatalf("reason = %#v, want %q", got, redact.ReasonKnownSensitiveKey)
	}
	marker, ok := value["marker"].(string)
	if !ok || !strings.HasPrefix(marker, "redacted:hmac-sha256:") {
		t.Fatalf("marker = %#v, want HMAC marker", value["marker"])
	}
	if strings.Contains(marker, "postgres://") {
		t.Fatalf("marker leaked raw value: %q", marker)
	}
}

func TestClassifyStackOutputPreservesNonSecretKeysAndRedactsSecrets(t *testing.T) {
	t.Parallel()

	key, err := redact.NewKey([]byte("aws-redaction-policy-test-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}

	tests := []struct {
		name       string
		key        string
		value      string
		wantRedact bool
		wantNoLeak string
	}{
		{name: "service endpoint preserved", key: "ServiceEndpoint", value: "https://api.example.com", wantRedact: false},
		{name: "exact password redacted", key: "Password", value: "hunter2", wantRedact: true, wantNoLeak: "hunter2"},
		{name: "compound pascalcase password redacted", key: "DatabasePassword", value: "leak-me", wantRedact: true, wantNoLeak: "leak-me"},
		{name: "compound secret redacted", key: "MyServiceSecret", value: "topsecret", wantRedact: true, wantNoLeak: "topsecret"},
		{name: "compound token redacted", key: "ApiTokenValue", value: "abc123", wantRedact: true, wantNoLeak: "abc123"},
		{name: "snake_case connection string redacted", key: "connection_string", value: "postgres://u:p@h/d", wantRedact: true, wantNoLeak: "postgres://"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			redacted, marker := ClassifyStackOutput(test.key, test.value, key)
			if redacted != test.wantRedact {
				t.Fatalf("ClassifyStackOutput(%q) redacted = %v, want %v", test.key, redacted, test.wantRedact)
			}
			if !test.wantRedact {
				if marker != nil {
					t.Fatalf("non-secret key %q returned marker %#v, want nil", test.key, marker)
				}
				return
			}
			if marker == nil {
				t.Fatalf("secret key %q returned nil marker", test.key)
			}
			if got := marker["reason"]; got != redact.ReasonKnownSensitiveKey {
				t.Fatalf("reason = %#v, want %q", got, redact.ReasonKnownSensitiveKey)
			}
			markerValue, ok := marker["marker"].(string)
			if !ok || !strings.HasPrefix(markerValue, "redacted:") {
				t.Fatalf("marker = %#v, want redacted marker", marker["marker"])
			}
			if test.wantNoLeak != "" && strings.Contains(markerValue, test.wantNoLeak) {
				t.Fatalf("marker leaked raw value %q: %q", test.wantNoLeak, markerValue)
			}
		})
	}
}

func TestAWSRedactStringFailsClosedForUnknownEnvironmentKeys(t *testing.T) {
	t.Parallel()

	key, err := redact.NewKey([]byte("aws-redaction-policy-test-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}

	value := RedactString(
		"public-looking-but-still-runtime-config",
		"ecs.task_definition.container.environment.LOG_LEVEL",
		key,
	)

	if got := value["ruleset_version"]; got != RedactionPolicyVersion {
		t.Fatalf("ruleset_version = %#v, want %q", got, RedactionPolicyVersion)
	}
	if got := value["reason"]; got != redact.ReasonUnknownProviderSchema {
		t.Fatalf("reason = %#v, want %q", got, redact.ReasonUnknownProviderSchema)
	}
	if got := value["marker"]; strings.Contains(got.(string), "public-looking") {
		t.Fatalf("marker leaked raw value: %q", got)
	}
}
