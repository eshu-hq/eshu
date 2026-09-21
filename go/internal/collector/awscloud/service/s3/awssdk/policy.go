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
)

// deriveBucketPolicyFlags parses one bucket policy document transiently and
// returns derived posture booleans only: whether any Allow statement grants
// access to a public principal ("*" or {"AWS":"*"}) and whether any Allow
// statement names a principal in an account other than ownerAccountID. The raw
// policy document, its statements, and principals never leave this function;
// only the two booleans are returned. A malformed document returns an error so
// the caller can record a scan warning rather than silently emitting a wrong
// posture.
//
// Both flags are non-nil whenever a document is parsed, so callers can persist
// an observed false (policy present, no public/cross-account grant) distinctly
// from an unknown (no policy present, nil).
func deriveBucketPolicyFlags(document, ownerAccountID string) (public *bool, crossAccount *bool, err error) {
	doc, decodeErr := decodeBucketPolicyDocument(document)
	if decodeErr != nil {
		return nil, nil, decodeErr
	}
	public, crossAccount = bucketPolicyFlagsFromDocument(doc, ownerAccountID)
	return public, crossAccount, nil
}

func bucketPolicyFlagsFromDocument(doc policyDocument, ownerAccountID string) (*bool, *bool) {
	owner := strings.TrimSpace(ownerAccountID)
	var grantsPublic, grantsCrossAccount bool
	for _, statement := range doc.Statements() {
		if !statement.isAllow() {
			continue
		}
		for _, principal := range statement.principalIdentifiers() {
			switch {
			case isPublicPrincipal(principal):
				grantsPublic = true
			case isCrossAccountPrincipal(principal, owner):
				grantsCrossAccount = true
			}
		}
	}
	return &grantsPublic, &grantsCrossAccount
}

// principalGrant is one bounded principal observation derived from a bucket
// policy. It intentionally excludes actions, resources, conditions, and the raw
// statement body.
type principalGrant struct {
	Kind             string
	Value            string
	AccountID        string
	Partition        string
	Service          string
	Outcome          string
	Public           bool
	CrossAccount     bool
	ServicePrincipal bool
	Unsupported      bool
	UnsupportedKey   string
	StatementSID     string
	PrincipalIsExact bool
}

// deriveBucketPolicyExternalPrincipalGrants parses one bucket policy document
// transiently and returns only bounded external-principal identity metadata.
// Same-account AWS principals are skipped, Deny statements are ignored, and
// unsupported principal types retain only their type key, not the raw value.
func deriveBucketPolicyExternalPrincipalGrants(document, ownerAccountID string) ([]principalGrant, error) {
	doc, err := decodeBucketPolicyDocument(document)
	if err != nil {
		return nil, err
	}
	return bucketPolicyExternalPrincipalGrantsFromDocument(doc, ownerAccountID), nil
}

func bucketPolicyExternalPrincipalGrantsFromDocument(doc policyDocument, ownerAccountID string) []principalGrant {
	owner := strings.TrimSpace(ownerAccountID)
	var grants []principalGrant
	seen := make(map[string]struct{})
	for _, statement := range doc.Statements() {
		if !statement.isAllow() {
			continue
		}
		for _, entry := range statement.principalEntries() {
			for _, grant := range grantsForPrincipalEntry(entry, owner, strings.TrimSpace(statement.SID)) {
				key := principalGrantIdentity(grant)
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				grants = append(grants, grant)
			}
		}
	}
	return grants
}

func decodeBucketPolicyDocument(document string) (policyDocument, error) {
	raw := strings.TrimSpace(document)
	if raw == "" {
		return policyDocument{}, fmt.Errorf("empty bucket policy document")
	}
	// AWS sometimes URL-encodes inline policy documents; the same handling the
	// IAM adapter uses for trust policies. Decode best-effort, then parse.
	if decoded, decodeErr := url.QueryUnescape(raw); decodeErr == nil && strings.Contains(decoded, "{") {
		raw = decoded
	}
	var doc policyDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return policyDocument{}, fmt.Errorf("parse bucket policy document: %w", err)
	}
	return doc, nil
}

// policyDocument is the minimal shape needed to derive posture booleans. It
// intentionally captures only Effect and Principal; resource, action, and
// condition detail are dropped so nothing sensitive is retained.
type policyDocument struct {
	Statement json.RawMessage `json:"Statement"`
}

// Statements normalizes the Statement field, which AWS allows to be either a
// single object or an array of objects.
func (d policyDocument) Statements() []policyStatement {
	trimmed := strings.TrimSpace(string(d.Statement))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var statements []policyStatement
		if err := json.Unmarshal(d.Statement, &statements); err != nil {
			return nil
		}
		return statements
	}
	var single policyStatement
	if err := json.Unmarshal(d.Statement, &single); err != nil {
		return nil
	}
	return []policyStatement{single}
}

// policyStatement captures the fields needed to derive principal posture and the
// normalized resource-policy permission statement. Action/Resource/Condition are
// parsed transiently to derive metadata-only fields (normalized action/resource
// patterns and condition KEY names); the raw statement body and condition values
// are never retained beyond this transient parse.
type policyStatement struct {
	SID         string          `json:"Sid"`
	Effect      string          `json:"Effect"`
	Principal   json.RawMessage `json:"Principal"`
	Action      json.RawMessage `json:"Action"`
	NotAction   json.RawMessage `json:"NotAction"`
	Resource    json.RawMessage `json:"Resource"`
	NotResource json.RawMessage `json:"NotResource"`
	Condition   json.RawMessage `json:"Condition"`
}

func (s policyStatement) isAllow() bool {
	return strings.EqualFold(strings.TrimSpace(s.Effect), "Allow")
}

type principalEntry struct {
	key    string
	values []string
}

// principalIdentifiers flattens the Principal field into its identifier strings.
// AWS encodes Principal as "*", a string, an array, or an object keyed by
// principal type (AWS, Service, Federated, CanonicalUser) whose values are a
// string or an array of strings. Service / Federated / CanonicalUser principals
// are AWS-internal trusts, not account principals, so only AWS-type and bare
// string identifiers feed public / cross-account derivation.
func (s policyStatement) principalIdentifiers() []string {
	var identifiers []string
	for _, entry := range s.principalEntries() {
		if entry.key == "AWS" {
			identifiers = append(identifiers, entry.values...)
		}
	}
	return identifiers
}

// principalEntries flattens the Principal field into deterministic key/value
// entries while keeping unsupported principal values out of returned metadata.
func (s policyStatement) principalEntries() []principalEntry {
	trimmed := strings.TrimSpace(string(s.Principal))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	// Principal: "*" or "arn:..." (a bare string).
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
	var entries []principalEntry
	for _, key := range orderedPrincipalKeys(asObject) {
		entries = append(entries, principalEntry{
			key:    strings.TrimSpace(key),
			values: rawStringList(asObject[key]),
		})
	}
	return entries
}

// rawStringList decodes a JSON value that is either a single string or an array
// of strings into a string slice.
func rawStringList(raw json.RawMessage) []string {
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return []string{single}
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err == nil {
		return many
	}
	return nil
}

func orderedPrincipalKeys(values map[string]json.RawMessage) []string {
	known := []string{"AWS", "Service", "Federated", "CanonicalUser"}
	var ordered []string
	seen := make(map[string]struct{}, len(values))
	for _, key := range known {
		if _, ok := values[key]; ok {
			ordered = append(ordered, key)
			seen[key] = struct{}{}
		}
	}
	var rest []string
	for key := range values {
		if _, ok := seen[key]; ok {
			continue
		}
		rest = append(rest, key)
	}
	sort.Strings(rest)
	return append(ordered, rest...)
}

func grantsForPrincipalEntry(entry principalEntry, owner, statementSID string) []principalGrant {
	switch entry.key {
	case "AWS":
		var grants []principalGrant
		for _, value := range entry.values {
			if grant, ok := grantForAWSPrincipal(value, owner, statementSID); ok {
				grants = append(grants, grant)
			}
		}
		return grants
	case "Service":
		var grants []principalGrant
		for _, value := range entry.values {
			service := strings.TrimSpace(value)
			if service == "" {
				continue
			}
			grants = append(grants, principalGrant{
				Kind:             awscloud.S3ExternalPrincipalKindAWSService,
				Value:            service,
				Service:          service,
				Outcome:          awscloud.S3ExternalPrincipalGrantOutcomeAWSService,
				ServicePrincipal: true,
				StatementSID:     statementSID,
				PrincipalIsExact: true,
			})
		}
		return grants
	default:
		key := strings.TrimSpace(entry.key)
		if key == "" {
			return nil
		}
		return []principalGrant{{
			Kind:           awscloud.S3ExternalPrincipalKindUnsupported,
			Value:          key,
			Outcome:        awscloud.S3ExternalPrincipalGrantOutcomeUnsupported,
			Unsupported:    true,
			UnsupportedKey: key,
			StatementSID:   statementSID,
		}}
	}
}

func grantForAWSPrincipal(identifier, owner, statementSID string) (principalGrant, bool) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return principalGrant{}, false
	}
	if isPublicPrincipal(identifier) {
		return principalGrant{
			Kind:         awscloud.S3ExternalPrincipalKindPublic,
			Value:        "*",
			Outcome:      awscloud.S3ExternalPrincipalGrantOutcomePublic,
			Public:       true,
			StatementSID: statementSID,
		}, true
	}
	accountID := accountFromPrincipal(identifier)
	if accountID != "" {
		if owner == "" || accountID == owner {
			return principalGrant{}, false
		}
		kind := awscloud.S3ExternalPrincipalKindAWSAccount
		partition := ""
		if strings.HasPrefix(identifier, "arn:") {
			kind = awscloud.S3ExternalPrincipalKindAWSARN
			partition = partitionFromPrincipal(identifier)
		}
		return principalGrant{
			Kind:             kind,
			Value:            identifier,
			AccountID:        accountID,
			Partition:        partition,
			Outcome:          awscloud.S3ExternalPrincipalGrantOutcomeCrossAccount,
			CrossAccount:     true,
			StatementSID:     statementSID,
			PrincipalIsExact: true,
		}, true
	}
	return principalGrant{
		Kind:           awscloud.S3ExternalPrincipalKindUnsupported,
		Value:          "AWS",
		Outcome:        awscloud.S3ExternalPrincipalGrantOutcomeUnsupported,
		Unsupported:    true,
		UnsupportedKey: "AWS",
		StatementSID:   statementSID,
	}, true
}

func principalGrantIdentity(grant principalGrant) string {
	return grant.Kind + "\x00" + grant.Value + "\x00" + grant.Outcome
}

// isPublicPrincipal reports whether a principal identifier grants access to
// everyone ("*").
func isPublicPrincipal(identifier string) bool {
	return strings.TrimSpace(identifier) == "*"
}

// isCrossAccountPrincipal reports whether a principal identifier names an AWS
// account other than owner. It accepts a 12-digit account id or an IAM/STS ARN
// whose account segment differs from owner. Identifiers without a resolvable
// account (and the public "*") are not treated as cross-account here.
func isCrossAccountPrincipal(identifier, owner string) bool {
	account := accountFromPrincipal(identifier)
	if account == "" || owner == "" {
		return false
	}
	return account != owner
}

// accountFromPrincipal extracts the 12-digit AWS account id from a principal
// identifier, returning "" when none is present. It handles a bare account id
// and an ARN of the form arn:<partition>:<service>:<region>:<account>:<resource>.
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

// partitionFromPrincipal extracts the ARN partition from a principal ARN.
func partitionFromPrincipal(identifier string) string {
	id := strings.TrimSpace(identifier)
	if !strings.HasPrefix(id, "arn:") {
		return ""
	}
	parts := strings.SplitN(id, ":", 3)
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimSpace(parts[1])
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
