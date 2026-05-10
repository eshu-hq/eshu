// Package tfstate records the reducer contract for Terraform state-derived
// canonical projection.
//
// This package owns the named Terraform-state reducer contract only:
// component names and readiness checkpoints used by ADR fixtures and downstream
// planning. Live source-local graph projection is implemented in
// internal/projector, because that service already owns committed facts to
// canonical graph nodes. RuntimeContractTemplate returns defensive copies so
// tests and ADR fixtures cannot mutate the shared contract.
package tfstate
