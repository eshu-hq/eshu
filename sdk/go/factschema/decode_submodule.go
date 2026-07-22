// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	submodulev1 "github.com/eshu-hq/eshu/sdk/go/factschema/submodule/v1"
)

// DecodeSubmodulePin decodes env.Payload into the latest submodulev1.Pin
// struct for the "submodule.pin" fact kind, dispatching on env.SchemaVersion
// major per Contract System v1 §3.2. Callers receive either the decoded
// struct or a classified *DecodeError; they must never substitute a
// zero-value struct on error. The git collector emits this fact kind
// (submodule.Emit, go/internal/collector/git_submodule_facts.go) and the
// reducer decodes it through this seam (decodeSubmodulePin,
// go/internal/reducer/factschema_decode_submodule.go) to materialize
// Repository-[:PINS_SUBMODULE]->Repository graph edges (issue #5420).
func DecodeSubmodulePin(env Envelope) (submodulev1.Pin, error) {
	return decodeLatestMajor[submodulev1.Pin](FactKindSubmodulePin, env)
}

// EncodeSubmodulePin marshals a submodulev1.Pin into the map[string]any
// payload shape an Envelope carries. It is the inverse of
// DecodeSubmodulePin for schema-version-1 payloads, used by this module's
// own round-trip tests.
func EncodeSubmodulePin(pin submodulev1.Pin) (map[string]any, error) {
	return encodeToPayload(pin)
}
