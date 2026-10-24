// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

import (
	runtimedeclarationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/runtimedeclaration/v1"
)

// DecodeRuntimeDeclarationVersion decodes env.Payload into the latest
// runtimedeclarationv1.Version struct for the "runtime_declaration.version"
// fact kind, dispatching on env.SchemaVersion major per Contract System v1
// §3.2. Callers receive either the decoded struct or a classified
// *DecodeError; they must never substitute a zero-value struct on error. The
// decoder does not check SpecKind membership, so a member added later still
// decodes. Issue #8815: the git collector emits this kind; no reducer or query
// handler decodes it until #8816 and #8817 land.
func DecodeRuntimeDeclarationVersion(env Envelope) (runtimedeclarationv1.Version, error) {
	return decodeLatestMajor[runtimedeclarationv1.Version](FactKindRuntimeDeclarationVersion, env)
}

// EncodeRuntimeDeclarationVersion marshals a runtimedeclarationv1.Version into
// the map[string]any payload shape an Envelope carries. It is the inverse of
// DecodeRuntimeDeclarationVersion for schema-version-1 payloads, used by the
// collector emitter and this module's own round-trip tests.
func EncodeRuntimeDeclarationVersion(version runtimedeclarationv1.Version) (map[string]any, error) {
	return encodeToPayload(version)
}
