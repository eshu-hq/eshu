// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

// IsSensitiveKeyName reports whether key matches the context-free sensitive
// name heuristic: a case-insensitive match against sensitiveQueryPattern
// (token|secret|password|credential|api[_-]?key|authorization) that is not
// carved out by the exact-name redactionSafePayloadKeys allowlist.
//
// This read-only predicate lets callers classify names before sharing a
// document. It uses the same regex and allowlist as validatePayloadKeys but
// has no fact kind, schema version, path, or value context. Fact validation
// can apply a narrower rule when all of that context is available.
func IsSensitiveKeyName(key string) bool {
	if _, safe := redactionSafePayloadKeys[key]; safe {
		return false
	}
	return sensitiveQueryPattern.MatchString(key)
}

// ValidateShareSafeKeys walks an arbitrary decoded JSON document (the result of
// json.Unmarshal into any) and rejects object keys that are sensitive-shaped
// per IsSensitiveKeyName at any depth.
//
// This context-free check does not require a map[string]any root or perform
// JSON-serializability checks. It calls validatePayloadKeys for an already
// decoded artifact. It cannot authorize the fact-specific Azure tag exception:
// a tagged Fact can admit an HMAC marker under an original sensitive-looking
// tag name, but an arbitrary shared document with that key is still rejected.
func ValidateShareSafeKeys(doc any) error {
	return validatePayloadKeys("", doc)
}
