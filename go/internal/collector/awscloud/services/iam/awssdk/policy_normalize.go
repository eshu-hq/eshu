// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	iamservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam"
	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

// normalizePolicyDocument parses one IAM identity policy document (inline or
// attached managed) into normalized, metadata-only PolicyStatement values.
//
// It captures only identifiers and patterns: effect, action/resource strings,
// statement SID, and the condition key/operator names (never the condition
// values, which can embed source IPs, tags, or other sensitive selectors). The
// raw JSON body is parsed and discarded; it is never returned or persisted.
func normalizePolicyDocument(raw, source, policyARN, policyName string) ([]iamservice.PolicyStatement, error) {
	document, err := decodePolicyDocument(raw)
	if err != nil {
		return nil, err
	}
	if document == nil {
		return nil, nil
	}
	rawStatements := documentStatements(document)
	statements := make([]iamservice.PolicyStatement, 0, len(rawStatements))
	for _, rawStatement := range rawStatements {
		statement, ok := normalizeIdentityStatement(rawStatement, source, policyARN, policyName)
		if ok {
			statements = append(statements, statement)
		}
	}
	if len(statements) == 0 {
		return nil, nil
	}
	return statements, nil
}

// normalizeTrustPolicyDocument parses a role trust / assume-role policy document
// into normalized trust statements. Each statement records the assume-role
// actions and the principals the statement grants assume-role to, with no raw
// JSON or condition values retained.
func normalizeTrustPolicyDocument(raw string) ([]iamservice.PolicyStatement, error) {
	document, err := decodePolicyDocument(raw)
	if err != nil {
		return nil, err
	}
	if document == nil {
		return nil, nil
	}
	rawStatements := documentStatements(document)
	statements := make([]iamservice.PolicyStatement, 0, len(rawStatements))
	for _, rawStatement := range rawStatements {
		statement, ok := normalizeTrustStatement(rawStatement)
		if ok {
			statements = append(statements, statement)
		}
	}
	if len(statements) == 0 {
		return nil, nil
	}
	return statements, nil
}

// decodePolicyDocument URL-decodes and JSON-parses a policy document. It returns
// (nil, nil) for a blank document so callers treat "no policy" as a non-error.
func decodePolicyDocument(raw string) (map[string]any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	decoded, err := url.QueryUnescape(raw)
	if err != nil {
		// Some documents arrive already decoded; fall back to the raw value
		// rather than failing the whole principal scan.
		decoded = raw
	}
	var document map[string]any
	if err := json.Unmarshal([]byte(decoded), &document); err != nil {
		return nil, fmt.Errorf("parse IAM policy document: %w", err)
	}
	return document, nil
}

// documentStatements returns the statement list of a policy document, tolerating
// the single-object form IAM allows ("Statement": {..}).
func documentStatements(document map[string]any) []map[string]any {
	switch typed := document["Statement"].(type) {
	case []any:
		statements := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if statement, ok := item.(map[string]any); ok {
				statements = append(statements, statement)
			}
		}
		return statements
	case map[string]any:
		return []map[string]any{typed}
	default:
		return nil
	}
}

// normalizeIdentityStatement maps one identity-policy statement object into a
// PolicyStatement. It returns ok=false for a statement with no effect so the
// caller skips malformed entries.
func normalizeIdentityStatement(statement map[string]any, source, policyARN, policyName string) (iamservice.PolicyStatement, bool) {
	effect := normalizeEffect(stringField(statement["Effect"]))
	if effect == "" {
		return iamservice.PolicyStatement{}, false
	}
	return iamservice.PolicyStatement{
		Source:             source,
		PolicyARN:          policyARN,
		PolicyName:         policyName,
		StatementSID:       stringField(statement["Sid"]),
		Effect:             effect,
		Actions:            stringList(statement["Action"]),
		NotActions:         stringList(statement["NotAction"]),
		Resources:          stringList(statement["Resource"]),
		NotResources:       stringList(statement["NotResource"]),
		ConditionKeys:      conditionKeys(statement["Condition"]),
		ConditionOperators: conditionOperators(statement["Condition"]),
	}, true
}

// normalizeTrustStatement maps one trust-policy statement object into a trust
// PolicyStatement, capturing the assume-role principals without their values'
// conditions.
func normalizeTrustStatement(statement map[string]any) (iamservice.PolicyStatement, bool) {
	effect := normalizeEffect(stringField(statement["Effect"]))
	if effect == "" {
		return iamservice.PolicyStatement{}, false
	}
	fingerprints, wildcard := webIdentitySubjectConditionSummary(statement["Condition"])
	return iamservice.PolicyStatement{
		Source:                         iamservice.PolicySourceTrust,
		Effect:                         effect,
		Actions:                        stringList(statement["Action"]),
		ConditionKeys:                  conditionKeys(statement["Condition"]),
		ConditionOperators:             conditionOperators(statement["Condition"]),
		AssumePrincipals:               trustStatementPrincipals(statement["Principal"]),
		WebIdentitySubjectFingerprints: fingerprints,
		WebIdentitySubjectWildcard:     wildcard,
	}, true
}

func webIdentitySubjectConditionSummary(value any) ([]string, bool) {
	operators, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	seen := make(map[string]struct{})
	wildcard := false
	for _, operatorValue := range operators {
		keyMap, ok := operatorValue.(map[string]any)
		if !ok {
			continue
		}
		for key, raw := range keyMap {
			if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(key)), ":sub") {
				continue
			}
			for _, subject := range stringList(raw) {
				fingerprint, subjectWildcard := webIdentitySubjectFingerprint(subject)
				if subjectWildcard {
					wildcard = true
					continue
				}
				if fingerprint != "" {
					seen[fingerprint] = struct{}{}
				}
			}
		}
	}
	if len(seen) == 0 {
		return nil, wildcard
	}
	fingerprints := make([]string, 0, len(seen))
	for fingerprint := range seen {
		fingerprints = append(fingerprints, fingerprint)
	}
	return fingerprints, wildcard
}

func webIdentitySubjectFingerprint(subject string) (string, bool) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return "", false
	}
	if strings.ContainsAny(subject, "*?[]") {
		return "", true
	}
	if !isExactKubernetesSubject(subject) {
		return "", false
	}
	return secretsiam.WebIdentitySubjectFingerprint(subject), false
}

func isExactKubernetesSubject(subject string) bool {
	subject = strings.TrimSpace(subject)
	parts := strings.Split(subject, ":")
	return len(parts) == 4 &&
		parts[0] == "system" &&
		parts[1] == "serviceaccount" &&
		strings.TrimSpace(parts[2]) != "" &&
		strings.TrimSpace(parts[3]) != ""
}

// trustStatementPrincipals flattens a statement's Principal element into the
// principal identifiers it names, reusing the trust-principal parser so the
// derived fact and the existing trust relationship agree on the principal set.
func trustStatementPrincipals(value any) []string {
	principals := trustPrincipalEntries(value)
	out := make([]string, 0, len(principals))
	for _, principal := range principals {
		identifier := strings.TrimSpace(principal.Identifier)
		if identifier != "" {
			out = append(out, identifier)
		}
	}
	return out
}

// conditionKeys returns the sorted, de-duplicated set of condition keys present
// on a statement's Condition element. It records only the keys (for example
// aws:SourceIp); the values are intentionally dropped so no sensitive selector
// is persisted.
func conditionKeys(value any) []string {
	operators, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	seen := make(map[string]struct{})
	for _, operatorValue := range operators {
		keyMap, ok := operatorValue.(map[string]any)
		if !ok {
			continue
		}
		for key := range keyMap {
			key = strings.TrimSpace(key)
			if key != "" {
				seen[key] = struct{}{}
			}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// conditionOperators returns the sorted, de-duplicated set of top-level
// Condition operators present on a statement. It records only operator names
// (for example StringEquals or ForAnyValue:StringLike); condition values and
// request-context values are intentionally dropped.
func conditionOperators(value any) []string {
	operators, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	seen := make(map[string]struct{})
	for operator := range operators {
		operator = strings.TrimSpace(operator)
		if operator != "" {
			seen[operator] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for operator := range seen {
		out = append(out, operator)
	}
	sort.Strings(out)
	return out
}

// normalizeEffect canonicalizes a statement Effect to "Allow" or "Deny", or ""
// for any other value.
func normalizeEffect(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allow":
		return "Allow"
	case "deny":
		return "Deny"
	default:
		return ""
	}
}

// stringField returns the trimmed string value of a JSON element, or "" when it
// is not a string.
func stringField(value any) string {
	if typed, ok := value.(string); ok {
		return strings.TrimSpace(typed)
	}
	return ""
}

// stringList coerces a JSON element that is either a string or an array of
// strings into a slice of non-empty trimmed strings. IAM Action/Resource
// elements use both forms interchangeably.
func stringList(value any) []string {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		return []string{trimmed}
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := item.(string); ok {
				trimmed := strings.TrimSpace(str)
				if trimmed != "" {
					out = append(out, trimmed)
				}
			}
		}
		return out
	default:
		return nil
	}
}
