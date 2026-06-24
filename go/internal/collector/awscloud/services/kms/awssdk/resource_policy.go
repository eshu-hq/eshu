// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	kmsservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/kms"
)

// deriveKeyPolicyResourcePermissionStatements parses one KMS key policy document
// transiently and returns one normalized, metadata-only ResourcePolicyStatement
// per statement. KMS key policies use the same JSON statement grammar as S3
// bucket policies, so the derivation mirrors the S3 resource-policy derivation:
// it captures effect, the raw action/resource patterns (the envelope builder
// lowercases/sorts them), the condition KEY names (never the values), and the
// derived grantee-principal facts. The raw policy document, statement bodies,
// and condition values never leave this function. A blank policy is a non-error
// empty result; a malformed document returns an error so the caller can record a
// scan warning rather than silently emitting wrong evidence.
func deriveKeyPolicyResourcePermissionStatements(document, ownerAccountID string) ([]kmsservice.ResourcePolicyStatement, error) {
	doc, present, err := decodeKeyPolicyDocument(document)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, nil
	}
	owner := strings.TrimSpace(ownerAccountID)
	statements := doc.statements()
	out := make([]kmsservice.ResourcePolicyStatement, 0, len(statements))
	for _, statement := range statements {
		effect := normalizeStatementEffect(statement.Effect)
		if effect == "" {
			continue
		}
		accountIDs, principalARNs, principalTypes, public, crossAccount := statement.derivePrincipalFacts(owner)
		out = append(out, kmsservice.ResourcePolicyStatement{
			StatementSID:        strings.TrimSpace(statement.SID),
			Effect:              effect,
			Actions:             rawStringList(statement.Action),
			NotActions:          rawStringList(statement.NotAction),
			Resources:           rawStringList(statement.Resource),
			NotResources:        rawStringList(statement.NotResource),
			ConditionKeys:       statementConditionKeys(statement.Condition),
			ConditionOperators:  statementConditionOperators(statement.Condition),
			PrincipalAccountIDs: accountIDs,
			PrincipalARNs:       principalARNs,
			PrincipalTypes:      principalTypes,
			IsPublic:            public,
			IsCrossAccount:      crossAccount,
		})
	}
	return out, nil
}

// decodeKeyPolicyDocument best-effort URL-decodes and JSON-parses a key policy
// document. It returns present=false for a blank document so callers treat "no
// policy" as a non-error.
func decodeKeyPolicyDocument(document string) (keyPolicyDocument, bool, error) {
	raw := strings.TrimSpace(document)
	if raw == "" {
		return keyPolicyDocument{}, false, nil
	}
	if decoded, decodeErr := url.QueryUnescape(raw); decodeErr == nil && strings.Contains(decoded, "{") {
		raw = decoded
	}
	var doc keyPolicyDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return keyPolicyDocument{}, false, fmt.Errorf("parse kms key policy document: %w", err)
	}
	return doc, true, nil
}

// keyPolicyDocument is the minimal shape needed to derive the normalized
// resource-policy statements. The raw statement bodies and condition values are
// parsed transiently and never retained.
type keyPolicyDocument struct {
	Statement json.RawMessage `json:"Statement"`
}

// statements normalizes the Statement field, which AWS allows to be either a
// single object or an array of objects.
func (d keyPolicyDocument) statements() []keyPolicyStatement {
	trimmed := strings.TrimSpace(string(d.Statement))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var statements []keyPolicyStatement
		if err := json.Unmarshal(d.Statement, &statements); err != nil {
			return nil
		}
		return statements
	}
	var single keyPolicyStatement
	if err := json.Unmarshal(d.Statement, &single); err != nil {
		return nil
	}
	return []keyPolicyStatement{single}
}

// keyPolicyStatement captures the fields needed to derive one normalized
// resource-policy statement. Action/Resource/Condition are parsed transiently;
// the raw body and condition values are never retained beyond this parse.
type keyPolicyStatement struct {
	SID         string          `json:"Sid"`
	Effect      string          `json:"Effect"`
	Principal   json.RawMessage `json:"Principal"`
	Action      json.RawMessage `json:"Action"`
	NotAction   json.RawMessage `json:"NotAction"`
	Resource    json.RawMessage `json:"Resource"`
	NotResource json.RawMessage `json:"NotResource"`
	Condition   json.RawMessage `json:"Condition"`
}

// principalEntry is one principal-type key and its values.
type principalEntry struct {
	key    string
	values []string
}

// principalEntries flattens the Principal element into key/value entries,
// tolerating the "*", bare-string, array, and object-by-type forms AWS allows.
func (s keyPolicyStatement) principalEntries() []principalEntry {
	trimmed := strings.TrimSpace(string(s.Principal))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var asString string
	if err := json.Unmarshal(s.Principal, &asString); err == nil {
		return []principalEntry{{key: "AWS", values: []string{asString}}}
	}
	if values := rawStringList(s.Principal); len(values) > 0 {
		return []principalEntry{{key: "AWS", values: values}}
	}
	var asObject map[string]json.RawMessage
	if err := json.Unmarshal(s.Principal, &asObject); err != nil {
		return nil
	}
	entries := make([]principalEntry, 0, len(asObject))
	for key, raw := range asObject {
		entries = append(entries, principalEntry{key: strings.TrimSpace(key), values: rawStringList(raw)})
	}
	return entries
}

// derivePrincipalFacts returns the derived grantee facts for one statement: the
// AWS account ids, the AWS principal ARNs, the principal-type set, and the
// public and cross-account booleans. Only AWS-type principals contribute account
// ids and ARNs.
func (s keyPolicyStatement) derivePrincipalFacts(owner string) (accountIDs, principalARNs, principalTypes []string, public, crossAccount bool) {
	accountSet := newStringSet()
	arnSet := newStringSet()
	typeSet := newStringSet()
	for _, entry := range s.principalEntries() {
		if principalType := principalTypeForKey(entry.key); principalType != "" {
			typeSet.add(principalType)
		}
		if entry.key != "AWS" {
			continue
		}
		for _, value := range entry.values {
			identifier := strings.TrimSpace(value)
			if identifier == "" {
				continue
			}
			if identifier == "*" {
				public = true
				continue
			}
			if strings.HasPrefix(identifier, "arn:") {
				arnSet.add(identifier)
			}
			if accountID := accountFromPrincipal(identifier); accountID != "" {
				accountSet.add(accountID)
				if owner != "" && accountID != owner {
					crossAccount = true
				}
			}
		}
	}
	return accountSet.sorted(), arnSet.sorted(), typeSet.sorted(), public, crossAccount
}

// principalTypeForKey maps a Principal element key to the lowercase
// resource-policy principal-type constant, or "" for an unrecognized key.
func principalTypeForKey(key string) string {
	switch strings.TrimSpace(key) {
	case "AWS":
		return awscloud.ResourcePolicyPrincipalTypeAWS
	case "Service":
		return awscloud.ResourcePolicyPrincipalTypeService
	case "Federated":
		return awscloud.ResourcePolicyPrincipalTypeFederated
	case "CanonicalUser":
		return awscloud.ResourcePolicyPrincipalTypeCanonical
	default:
		return ""
	}
}

// statementConditionKeys returns the de-duplicated, sorted condition KEY names
// present on a statement's Condition element. The values are intentionally
// dropped so no sensitive selector is persisted.
func statementConditionKeys(raw json.RawMessage) []string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var operators map[string]json.RawMessage
	if err := json.Unmarshal(raw, &operators); err != nil {
		return nil
	}
	keys := newStringSet()
	for _, operatorRaw := range operators {
		var keyMap map[string]json.RawMessage
		if err := json.Unmarshal(operatorRaw, &keyMap); err != nil {
			continue
		}
		for key := range keyMap {
			if trimmedKey := strings.TrimSpace(key); trimmedKey != "" {
				keys.add(trimmedKey)
			}
		}
	}
	return keys.sorted()
}

// statementConditionOperators returns the de-duplicated, sorted condition
// operator names present on a statement's Condition element. It records only the
// top-level operator keys (for example StringEquals or ForAnyValue:StringLike);
// condition values are intentionally dropped.
func statementConditionOperators(raw json.RawMessage) []string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var operators map[string]json.RawMessage
	if err := json.Unmarshal(raw, &operators); err != nil {
		return nil
	}
	operatorSet := newStringSet()
	for operator := range operators {
		if trimmedOperator := strings.TrimSpace(operator); trimmedOperator != "" {
			operatorSet.add(trimmedOperator)
		}
	}
	return operatorSet.sorted()
}

// normalizeStatementEffect canonicalizes a statement Effect to "Allow" or
// "Deny", or "" for any other value so the caller skips malformed statements.
func normalizeStatementEffect(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "allow":
		return "Allow"
	case "deny":
		return "Deny"
	default:
		return ""
	}
}

// rawStringList coerces a JSON element that is either a single string or an
// array of strings into a slice of non-empty trimmed strings. IAM/KMS
// Action/Resource/Principal elements use both forms interchangeably.
func rawStringList(raw json.RawMessage) []string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		if value := strings.TrimSpace(single); value != "" {
			return []string{value}
		}
		return nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		out := make([]string, 0, len(many))
		for _, value := range many {
			if trimmedValue := strings.TrimSpace(value); trimmedValue != "" {
				out = append(out, trimmedValue)
			}
		}
		return out
	}
	return nil
}

// accountFromPrincipal extracts the 12-digit AWS account id from a principal
// identifier (a bare account id or an IAM/STS ARN), returning "" when none is
// present.
func accountFromPrincipal(identifier string) string {
	id := strings.TrimSpace(identifier)
	if id == "" || id == "*" {
		return ""
	}
	if isAccountID(id) {
		return id
	}
	if strings.HasPrefix(id, "arn:") {
		parts := strings.SplitN(id, ":", 6)
		if len(parts) >= 5 {
			if account := strings.TrimSpace(parts[4]); isAccountID(account) {
				return account
			}
		}
	}
	return ""
}

// isAccountID reports whether value is exactly a 12-digit AWS account id.
func isAccountID(value string) bool {
	if len(value) != 12 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

// stringSet is a small de-duplicating ordered-output set used while deriving the
// bounded principal and condition-summary projections.
type stringSet struct {
	seen map[string]struct{}
}

func newStringSet() *stringSet {
	return &stringSet{seen: map[string]struct{}{}}
}

func (s *stringSet) add(value string) {
	s.seen[value] = struct{}{}
}

func (s *stringSet) sorted() []string {
	if len(s.seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.seen))
	for value := range s.seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
