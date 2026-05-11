package reducer

import (
	"context"
	"errors"
)

// TerraformConfigStateDriftHandler reconciles Terraform config facts (parsed
// HCL) against Terraform state facts to emit drift candidates. The handler
// joins the two scopes via the tfstatebackend resolver, builds one Candidate
// per drifted address (carrying cross-scope EvidenceAtoms), and hands the
// candidate slice to the correlation engine to record the deterministic
// explain trace.
//
// Phase 0 status: stub. The Handle method returns errHandlerNotImplemented
// until Phase 1 (Agent A) wires the resolver, classifier helpers, and
// telemetry counters declared in
// go/internal/correlation/rules/tfconfigstatedrift.
type TerraformConfigStateDriftHandler struct{}

// errHandlerNotImplemented is returned by the Phase 0 handler stub. The
// reducer runtime never invokes this handler because the new
// DomainConfigStateDrift is not yet registered in
// implementedDefaultDomainDefinitions (Phase 1 — Agent B).
var errHandlerNotImplemented = errors.New("terraform_config_state_drift handler not implemented")

// Handle satisfies the reducer handler contract. The Phase 0 stub returns
// errHandlerNotImplemented; Phase 1 implements the join-classify-evaluate
// pipeline.
func (h TerraformConfigStateDriftHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	_ = ctx
	_ = intent
	return Result{}, errHandlerNotImplemented
}
