// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package factschema

// The submodule family fact-kind string is DOTTED, matching the incident/
// kubernetes_live/oci_registry/package_registry/sbom_attestation/work_item/
// observability convention (fact_kinds.go). The dot is part of the wire kind
// go/internal/facts.SubmodulePinFactKind declares; this value MATCHES that
// wire string byte-for-byte and never invents or renames the namespace.
// TestFactSchemaKindsMatchWireFactKinds (reducer side) asserts it stays
// byte-equal to its facts.SubmodulePinFactKind counterpart. The git
// collector emits this kind and the reducer decodes and projects it into
// Repository-[:PINS_SUBMODULE]->Repository graph edges (issue #5420); the
// kind remains typed and schema-drift-locked.

// FactKindSubmodulePin is the "submodule.pin" fact kind.
const FactKindSubmodulePin = "submodule.pin"
