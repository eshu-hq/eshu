// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

const (
	// RedactionPolicyVersion identifies the AWS launch scanner sensitive-key
	// policy attached to redacted fact payload values.
	RedactionPolicyVersion = "aws-launch-2026-05-14"
)

var awsRedactionRules = mustAWSRedactionRules()

// awsSensitiveKeySet is the membership index of awsSensitiveKeys used by
// CloudFormation output-key token matching. It mirrors the slice exactly so the
// policy stays single-sourced.
var awsSensitiveKeySet = mustSensitiveKeySet()

// RedactString returns the shared AWS redaction payload for a scalar string.
//
// AWS environment values are treated as unknown provider schema, so callers
// preserve key names but redact all runtime values. Known sensitive key names
// receive a stronger reason label for audit and downstream proof.
func RedactString(raw string, source string, key redact.Key) map[string]any {
	decision := awsRedactionRules.Classify(source, redact.SchemaUnknown, redact.FieldScalar)
	value := redact.String(raw, decision.Reason, decision.Source, key)
	return map[string]any{
		"marker":          value.Marker,
		"reason":          value.Reason,
		"source":          value.Source,
		"ruleset_version": decision.RuleSetVersion,
	}
}

// ClassifyStackOutput decides whether one named output value is secret-like and
// must be redacted. Output keys are author-controlled CloudFormation
// identifiers (e.g. "DatabasePassword", "ApiTokenValue"), so this classifier is
// stricter than the shared RuleSet path: it redacts when the whole key matches
// the sensitive-key policy OR when any sub-token of the key (split on case and
// separators) matches a sensitive key. This closes the gap where a compound
// PascalCase key such as "DatabasePassword" would otherwise be preserved
// because it is not an exact sensitive-key match.
//
// The bool reports whether the value was redacted, and the returned map carries
// the redaction marker payload when redacted (nil otherwise). CloudFormation
// uses this to keep secret-shaped stack outputs (passwords, tokens, connection
// strings) out of durable facts while preserving non-secret outputs such as
// service endpoints.
func ClassifyStackOutput(key string, value string, redactionKey redact.Key) (redacted bool, marker map[string]any) {
	source := strings.TrimSpace(key)
	decision := awsRedactionRules.Classify(source, redact.SchemaKnown, redact.FieldScalar)
	if decision.Action == redact.ActionPreserve && !keyTokenIsSensitive(source) {
		return false, nil
	}
	if decision.Reason != redact.ReasonKnownSensitiveKey {
		decision.Reason = redact.ReasonKnownSensitiveKey
	}
	value = redact.String(value, decision.Reason, decision.Source, redactionKey).Marker
	return true, map[string]any{
		"marker":          value,
		"reason":          decision.Reason,
		"source":          decision.Source,
		"ruleset_version": decision.RuleSetVersion,
	}
}

// keyTokenIsSensitive reports whether any contiguous run of case/separator
// sub-tokens of a CloudFormation output key matches the shared AWS sensitive-key
// policy. It checks every contiguous token window (joined with "_"), not only
// single tokens, so compound policy entries are caught even when the output key
// wraps them with extra words. Examples it must redact:
//
//   - "DatabasePassword" via the single token "password"
//   - "ApiKeyValue" via the window "api_key"
//   - "DatabaseConnectionString" via the window "connection_string"
//   - "ServiceAccessKeyId" via the window "access_key_id"
//
// Single-token matching alone missed the multi-word policy entries (api_key,
// connection_string, access_key_id, ...) because no single token equals them.
// A non-secret key such as "KeyName" or "RoleArn" still has no matching window
// and stays preserved.
func keyTokenIsSensitive(key string) bool {
	tokens := splitIdentifierTokens(key)
	var window strings.Builder
	for start := range tokens {
		window.Reset()
		for index := start; index < len(tokens); index++ {
			if index > start {
				window.WriteByte('_')
			}
			window.WriteString(tokens[index])
			if _, ok := awsSensitiveKeySet[window.String()]; ok {
				return true
			}
		}
	}
	return false
}

// splitIdentifierTokens lowercases and splits a key on camelCase boundaries and
// common separators into its component words.
func splitIdentifierTokens(key string) []string {
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	runes := []rune(key)
	for index, char := range runes {
		switch {
		case char == '_' || char == '-' || char == '.' || char == '/' || char == ' ':
			flush()
		case char >= 'A' && char <= 'Z':
			if index > 0 {
				previous := runes[index-1]
				next := rune(0)
				if index+1 < len(runes) {
					next = runes[index+1]
				}
				if (previous >= 'a' && previous <= 'z') || (previous >= 'A' && previous <= 'Z' && next >= 'a' && next <= 'z') {
					flush()
				}
			}
			current.WriteRune(char - 'A' + 'a')
		default:
			current.WriteRune(char)
		}
	}
	flush()
	return tokens
}

func mustAWSRedactionRules() redact.RuleSet {
	rules, err := redact.NewRuleSet(RedactionPolicyVersion, awsSensitiveKeys)
	if err != nil {
		panic(err)
	}
	return rules
}

func mustSensitiveKeySet() map[string]struct{} {
	set := make(map[string]struct{}, len(awsSensitiveKeys))
	for _, key := range awsSensitiveKeys {
		set[strings.ToLower(strings.TrimSpace(key))] = struct{}{}
	}
	return set
}

var awsSensitiveKeys = []string{
	"access_key",
	"access_key_id",
	"api_key",
	"apikey",
	"auth_token",
	"authorization",
	"aws_access_key_id",
	"aws_secret_access_key",
	"aws_session_token",
	"bearer_token",
	"client_secret",
	"connection_string",
	"credential",
	"credentials",
	"database_url",
	"db_password",
	"db_url",
	"dsn",
	"encryption_key",
	"github_token",
	"jdbc_url",
	"mongo_uri",
	"passphrase",
	"password",
	"passwd",
	"private_key",
	"pwd",
	"redis_url",
	"secret",
	"secret_access_key",
	"secret_key",
	"session_token",
	"signing_key",
	"slack_webhook_url",
	"stripe_secret_key",
	"token",
	"webhook_url",
}
