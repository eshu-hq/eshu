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
//
// EvaluateBackendConfig derives the config-side ownership-join candidate for
// one parsed `terraform { backend "<kind>" {} }` block. A bare
// `backend "local" {}` with no `path` attribute applies Terraform's own
// default ("terraform.tfstate" relative to the root module directory —
// https://developer.hashicorp.com/terraform/language/backend/local) rather
// than producing no candidate (issue #5594). A BackendLocal candidate's
// locator must be an absolute path that matches the absolute path
// LocalStateSource actually opens, so BackendConfigContext.RepoLocalPath (the
// repository checkout root) is required; without it, EvaluateBackendConfig
// reports no candidate rather than guess a locator. Only BackendS3 and
// BackendLocal produce candidates; any other Terraform backend kind (gcs,
// azurerm, remote, http, ...) is unmodeled here and yields neither a
// candidate nor a warning. DiscoveryCandidate.LocatorDefaulted marks the
// implicit-default case so a downstream successful resolution can log
// "resolved via default" distinctly from "resolved via explicit path"; it
// flows through tfstatebackend.TerraformBackendRow and CommitAnchor to the
// terraform_config_state_drift reducer handler.
package terraformstate
