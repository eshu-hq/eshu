// Package tfstatebackend joins Terraform state snapshots to the config repo
// commit that declared their backend.
//
// Design contract: docs/superpowers/plans/2026-05-10-tfstate-config-state-drift-design.md
// Tracking issue: #43 (epic #50).
//
// State and config facts live in separate scopes: state in state_snapshot
// (go/internal/scope/tfstate.go:33-40), config in the owning repo's snapshot
// scope. The drift correlation pack in
// go/internal/correlation/rules/tfconfigstatedrift needs both sides on the
// same Candidate. This package resolves the (backend_kind, locator_hash)
// composite key to the latest sealed config snapshot that emitted a matching
// terraform_backends parser fact, and attaches prior-generation state
// evidence so the classifier can detect removed_from_state.
//
// V1 single-owner policy: at most one config repo may claim a given
// (backend_kind, locator_hash). Ambiguous ownership returns a typed error and
// the drift handler rejects the candidate with structural_mismatch. Multi
// owner resolution is a follow-up ADR.
package tfstatebackend
