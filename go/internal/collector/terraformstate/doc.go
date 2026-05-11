// Package terraformstate reads Terraform state snapshots into redacted facts.
//
// The package keeps raw Terraform state inside source readers and parser-local
// windows only. Callers receive typed fact envelopes and redaction evidence, not
// raw state bytes or unredacted attribute values.
//
// Terragrunt remote_state blocks are resolved into DiscoveryCandidate rows by
// TerragruntRemoteStateCandidate. The resolver always emits the underlying
// backend kind (BackendS3 or BackendLocal); discovery never observes
// BackendTerragrunt because the Terragrunt indirection is config-time only.
// Local-backend rows additionally require the repository checkout's local
// path so the resolver can compute a repo-relative RelativePath and reject
// backend paths that escape the checkout.
package terraformstate
