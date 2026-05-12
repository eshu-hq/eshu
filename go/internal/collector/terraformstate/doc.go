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
