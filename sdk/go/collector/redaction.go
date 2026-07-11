// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

// IsSensitiveKeyName reports whether key matches the same sensitive-key-name
// heuristic validatePayloadKeys applies at fact-emission time: a
// case-insensitive match against sensitiveQueryPattern
// (token|secret|password|credential|api[_-]?key|authorization) that is not
// carved out by the exact-name redactionSafePayloadKeys allowlist.
//
// This is a read-only predicate, not a validator: it answers "would this key
// name be rejected" for a caller that wants to redact or classify a field
// itself (for example a wrong-answer report bundle deciding which JSON keys to
// mask) without duplicating or drifting from the fail-closed rule collectors
// are held to. The underlying regex and allowlist stay unexported in
// validation.go; this function is a thin, behavior-preserving export over the
// exact same predicate validatePayloadKeys evaluates per key, so a future edit
// to the rule cannot silently diverge between the two call sites.
func IsSensitiveKeyName(key string) bool {
	if _, safe := redactionSafePayloadKeys[key]; safe {
		return false
	}
	return sensitiveQueryPattern.MatchString(key)
}

// ValidateShareSafeKeys walks an arbitrary decoded JSON document (the result of
// json.Unmarshal into `any`) and fails closed if any object key at any depth is
// sensitive-shaped per IsSensitiveKeyName, exactly as validatePayloadKeys does
// for a collector Fact.Payload.
//
// Unlike validatePayload, ValidateShareSafeKeys does not require a
// map[string]any root and does not perform JSON-serializability checks — it is
// meant to gate an already-serialized artifact (for example a finished
// wrong-answer report bundle) rather than a collector payload before it is
// wrapped in a Fact. The recursive walk itself is byte-for-byte the same rule:
// this function calls validatePayloadKeys directly so a fact payload and a
// share-safe artifact can never disagree about which key names are allowed.
func ValidateShareSafeKeys(doc any) error {
	return validatePayloadKeys("", doc)
}
