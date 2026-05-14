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
