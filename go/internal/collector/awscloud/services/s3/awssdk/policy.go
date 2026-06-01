package awssdk

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
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
	raw := strings.TrimSpace(document)
	if raw == "" {
		return nil, nil, fmt.Errorf("empty bucket policy document")
	}
	// AWS sometimes URL-encodes inline policy documents; the same handling the
	// IAM adapter uses for trust policies. Decode best-effort, then parse.
	if decoded, decodeErr := url.QueryUnescape(raw); decodeErr == nil && strings.Contains(decoded, "{") {
		raw = decoded
	}
	var doc policyDocument
	if unmarshalErr := json.Unmarshal([]byte(raw), &doc); unmarshalErr != nil {
		return nil, nil, fmt.Errorf("parse bucket policy document: %w", unmarshalErr)
	}
	owner := strings.TrimSpace(ownerAccountID)
	var grantsPublic, grantsCrossAccount bool
	for _, statement := range doc.Statements() {
		if !strings.EqualFold(strings.TrimSpace(statement.Effect), "Allow") {
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
	return &grantsPublic, &grantsCrossAccount, nil
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

// policyStatement captures only the fields needed to derive principal posture.
type policyStatement struct {
	Effect    string          `json:"Effect"`
	Principal json.RawMessage `json:"Principal"`
}

// principalIdentifiers flattens the Principal field into its identifier strings.
// AWS encodes Principal as "*", a string, an array, or an object keyed by
// principal type (AWS, Service, Federated, CanonicalUser) whose values are a
// string or an array of strings. Service / Federated / CanonicalUser principals
// are AWS-internal trusts, not account principals, so only AWS-type and bare
// string identifiers feed public / cross-account derivation.
func (s policyStatement) principalIdentifiers() []string {
	trimmed := strings.TrimSpace(string(s.Principal))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	// Principal: "*" or "arn:..." (a bare string).
	var asString string
	if err := json.Unmarshal(s.Principal, &asString); err == nil {
		return []string{asString}
	}
	var asObject map[string]json.RawMessage
	if err := json.Unmarshal(s.Principal, &asObject); err != nil {
		return nil
	}
	values, ok := asObject["AWS"]
	if !ok {
		return nil
	}
	return rawStringList(values)
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
