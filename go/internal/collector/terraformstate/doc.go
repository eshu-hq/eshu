// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package terraformstate reads Terraform state snapshots into redacted facts.
//
// The package owns exact state discovery primitives, read-only source
// interfaces, packaged provider-schema coverage for resource and data-source
// attributes, streaming snapshot identity reads, parser redaction, composite
// capture observation, and Terraform-state fact envelope output. It does not
// schedule claims, choose cloud credentials, commit facts, write graph rows, or
// call cloud SDKs directly.
//
// Raw Terraform state must stay inside StateSource readers and parser-local
// windows. Callers receive typed fact envelopes, redaction evidence, bounded
// parse summaries, and classified warning counts, not raw state bytes or
// unredacted attribute values. Warning facts carry stable reason codes plus
// severity/actionability for recognized guardrail, provider-schema, source
// missing, backend-expression, and tag-normalization cases. Git-observed
// backend config that cannot become an exact candidate is represented as
// warning evidence, not as a StateKey.
// The parser also emits applied incident-routing source facts for allowlisted
// PagerDuty and alert-route resources observed in state. Those facts preserve
// Terraform address, module, provider, state generation, and fingerprinted or
// redacted routing metadata; reducers own declared/applied/observed comparison
// and graph/read-model truth.
//
// LocatorHash and ScopeLocatorHash are separate contracts. LocatorHash includes
// backend kind, locator, and version ID for per-candidate identity.
// ScopeLocatorHash includes backend kind and locator only for the
// version-agnostic join key shared with scope.NewTerraformStateSnapshotScope.
package terraformstate
