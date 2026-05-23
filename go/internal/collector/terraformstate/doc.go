// Package terraformstate reads Terraform state snapshots into redacted facts.
//
// The package owns exact state discovery primitives, read-only source
// interfaces, streaming snapshot identity reads, parser redaction, composite
// capture observation, and Terraform-state fact envelope output. It does not
// schedule claims, choose cloud credentials, commit facts, write graph rows, or
// call cloud SDKs directly.
//
// Raw Terraform state must stay inside StateSource readers and parser-local
// windows. Callers receive typed fact envelopes, redaction evidence, bounded
// parse summaries, and warning counts, not raw state bytes or unredacted
// attribute values.
//
// LocatorHash and ScopeLocatorHash are separate contracts. LocatorHash includes
// backend kind, locator, and version ID for per-candidate identity.
// ScopeLocatorHash includes backend kind and locator only for the
// version-agnostic join key shared with scope.NewTerraformStateSnapshotScope.
package terraformstate
