// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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
		// Compound keys that wrap a multi-word sensitive key with extra
		// prefix/suffix words must still redact. Single-token matching missed
		// these because the policy entry (api_key, connection_string) is itself
		// a multi-word key that no single token equals.
		{name: "compound api key value redacted", key: "ApiKeyValue", value: "ak-leak-me", wantRedact: true, wantNoLeak: "ak-leak-me"},
		{name: "wrapped connection string redacted", key: "DatabaseConnectionString", value: "postgres://u:p@h/d", wantRedact: true, wantNoLeak: "postgres://"},
		{name: "prefixed connection string redacted", key: "PrimaryConnectionString", value: "amqps://u:p@h", wantRedact: true, wantNoLeak: "amqps://"},
		{name: "wrapped access key id redacted", key: "ServiceAccessKeyId", value: "AKIA-leak", wantRedact: true, wantNoLeak: "AKIA-leak"},
		// Keys that merely contain the word "key" or "url" as a non-secret token
		// must stay preserved; the n-gram match must not over-redact identifiers
		// and ARNs.
		{name: "key name preserved", key: "KeyName", value: "my-ssh-keypair", wantRedact: false},
		{name: "public key id preserved", key: "KeyId", value: "1234-abcd", wantRedact: false},
		{name: "role arn preserved", key: "RoleArn", value: "arn:aws:iam::1:role/app", wantRedact: false},
		{name: "website url preserved", key: "WebsiteUrl", value: "https://example.com", wantRedact: false},
		// Tokens added when the shared awsSensitiveKeys policy was widened:
		// outputs named exactly these were preserved in cleartext before.
		{name: "credentials redacted", key: "Credentials", value: "user:pass", wantRedact: true, wantNoLeak: "user:pass"},
		{name: "compound credential redacted", key: "ServiceCredential", value: "cred-leak", wantRedact: true, wantNoLeak: "cred-leak"},
		{name: "encryption key redacted", key: "EncryptionKey", value: "ek-leak", wantRedact: true, wantNoLeak: "ek-leak"},
		{name: "signing key redacted", key: "SigningKey", value: "sk-leak", wantRedact: true, wantNoLeak: "sk-leak"},
		{name: "passphrase redacted", key: "Passphrase", value: "pp-leak", wantRedact: true, wantNoLeak: "pp-leak"},
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
