// Package tfstate records the reducer contract for Terraform state-derived
// canonical projection.
//
// The package is currently a scaffold: it names the projector components and
// readiness checkpoints that future Terraform state collector and reducer work
// must honor. RuntimeContractTemplate returns defensive copies for tests and
// ADR fixtures. No live projection logic lives here yet.
package tfstate
