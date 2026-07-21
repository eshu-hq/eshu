// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	codeownersv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codeowners/v1"
)

// DecodeCodeownersOwnership decodes env.Payload into the latest
// codeownersv1.Ownership struct for the "codeowners.ownership" fact kind,
// dispatching on env.SchemaVersion major per Contract System v1 §3.2.
// Callers receive either the decoded struct or a classified *DecodeError;
// they must never substitute a zero-value struct on error. Issue #5419
// Phase 1: no reducer or query handler calls this yet — it exists so the
// checked-in schema and fixture pack are honest against a real decode path
// ahead of the consumer landing in a later phase.
func DecodeCodeownersOwnership(env Envelope) (codeownersv1.Ownership, error) {
	return decodeLatestMajor[codeownersv1.Ownership](FactKindCodeownersOwnership, env)
}

// EncodeCodeownersOwnership marshals a codeownersv1.Ownership into the
// map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeCodeownersOwnership for schema-version-1 payloads, used by this
// module's own round-trip tests.
func EncodeCodeownersOwnership(ownership codeownersv1.Ownership) (map[string]any, error) {
	return encodeToPayload(ownership)
}
