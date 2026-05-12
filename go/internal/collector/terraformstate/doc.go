// Package terraformstate reads Terraform state snapshots into redacted facts.
//
// The package keeps raw Terraform state inside source readers and parser-local
// windows only. Callers receive typed fact envelopes and redaction evidence,
// not raw state bytes or unredacted attribute values. ParseResult and
// ParseStreamResult also surface OutputFacts, ModuleFacts, and a WarningsByKind
// breakdown so the runtime can record per-locator emission counters without
// rescanning the streamed envelopes.
//
// Terragrunt remote_state blocks are resolved into DiscoveryCandidate rows by
// TerragruntRemoteStateCandidate. The resolver always emits the underlying
// backend kind (BackendS3 or BackendLocal); discovery never observes
// BackendTerragrunt because the Terragrunt indirection is config-time only.
// Local-backend rows additionally require the repository checkout's local
// path so the resolver can compute a repo-relative RelativePath and reject
// backend paths that escape the checkout.
//
// Composite Terraform-state attributes (nested blocks, repeated blocks)
// flow through two paths. When ProviderSchemaResolver covers the
// (resourceType, attributeKey) pair, readCompositeValue walks the JSON
// subtree through the same streaming decoder the rest of the parser
// drives and emits the nested-singleton-array shape the loader's
// flattener expects. Every scalar leaf inside the captured composite
// is still classified through RedactionRules.Classify so a
// sensitive-key segment (e.g., "password" under aws_iam_user.
// login_profile) is HMAC-stamped at the leaf. Composites the resolver
// does not cover, composites the top-level redaction decision drops, and
// schema-known composites the walker cannot consume are all reported through
// the caller-supplied CompositeCaptureRecorder with a bounded reason label.
// ADR
// 2026-05-12-tfstate-parser-composite-capture-for-schema-known-paths
// owns the contract.
//
// LocatorHash and ScopeLocatorHash are intentionally distinct. LocatorHash
// digests (BackendKind, Locator, VersionID) and backs per-version identity
// — CandidatePlanningID and the persisted
// terraform_state_snapshot.payload->>'locator_hash' field. ScopeLocatorHash
// digests (BackendKind, Locator) only and is the version-agnostic join key
// shared with scope.NewTerraformStateSnapshotScope; the drift resolver
// compares the two byte-for-byte. Use ScopeLocatorHash whenever the call
// site needs the join key the resolver receives at runtime; use LocatorHash
// only for per-candidate identity. Issue #203 documents the silent
// drift-rejection bug that motivated the split.
package terraformstate
