// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	s3service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/s3"
)

// deriveBucketPolicyResourcePermissionStatements parses one bucket policy
// document transiently and returns one normalized, metadata-only
// ResourcePolicyStatement per statement. It is the resource-side analog of the
// IAM identity-policy statement normalization: it captures effect, the raw
// action/resource patterns (the envelope builder lowercases/sorts them), the
// condition KEY names (never the values), and the derived grantee-principal
// facts. The raw policy document, statement bodies, and condition values never
// leave this function. A malformed document returns an error so the caller can
// record a scan warning rather than silently emitting wrong evidence.
func deriveBucketPolicyResourcePermissionStatements(document, ownerAccountID string) ([]s3service.ResourcePolicyStatement, error) {
	doc, err := decodeBucketPolicyDocument(document)
	if err != nil {
		return nil, err
	}
	return bucketPolicyResourcePermissionStatementsFromDocument(doc, ownerAccountID), nil
}

func bucketPolicyResourcePermissionStatementsFromDocument(doc policyDocument, ownerAccountID string) []s3service.ResourcePolicyStatement {
	owner := strings.TrimSpace(ownerAccountID)
	statements := doc.Statements()
	out := make([]s3service.ResourcePolicyStatement, 0, len(statements))
	for _, statement := range statements {
		effect := normalizeStatementEffect(statement.Effect)
		if effect == "" {
			continue
		}
		accountIDs, principalARNs, principalTypes, public, crossAccount := derivePrincipalFacts(statement, owner)
		out = append(out, s3service.ResourcePolicyStatement{
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
	return out
}

// derivePrincipalFacts walks a statement's Principal element and returns the
// derived grantee facts: the de-duplicated AWS account ids, the AWS principal
// ARNs, the principal-type set (aws / service / federated / canonical), and the
// public and cross-account booleans. Only AWS-type principals contribute account
// ids and ARNs; service/federated/canonical principals contribute only their
// type.
func derivePrincipalFacts(statement policyStatement, owner string) (accountIDs, principalARNs, principalTypes []string, public, crossAccount bool) {
	accountSet := newStringSet()
	arnSet := newStringSet()
	typeSet := newStringSet()
	for _, entry := range statement.principalEntries() {
		principalType := principalTypeForKey(entry.key)
		if principalType != "" {
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
			if isPublicPrincipal(identifier) {
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

// normalizeStatementEffect canonicalizes a statement Effect to "Allow" or
// "Deny", or "" for any other value so the caller skips malformed statements.
// The envelope builder re-validates the effect; this gate just drops statements
// that name no usable effect before they reach the scanner.
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

// principalTypeForKey maps a Principal element key to the lowercase
// resource-policy principal-type constant, or "" for an unrecognized key (which
// contributes no type rather than leaking the raw key).
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
// present on a statement's Condition element. It records only the keys (for
// example aws:SourceIp); the values are intentionally dropped so no sensitive
// selector is persisted.
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

// stringSet is a small de-duplicating ordered-output set used while deriving
// the bounded principal and condition-summary projections.
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
