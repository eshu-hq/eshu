terraform {
  required_version = ">= 1.5.0"
}

# Issue #5572 golden-corpus fixture for the "derived" outcome. This directory
# sits at the EXACT path modules.tf's
#   module "vpc" { source = "terraform-aws-modules/vpc/aws" }
# resolves to under path.Clean(path.Join(callSiteDir, source)) when treated
# as a local relative path -- but classifyModuleSource's Terraform-Registry-
# shorthand heuristic (three slash-separated segments, no scheme, no leading
# ".") classifies that source as an EXTERNAL REGISTRY reference instead
# (go/internal/storage/postgres/tfstate_drift_evidence_module_prefix.go), so
# the module {} block that names this directory is never walked as a
# resolved local callee -- even though this directory genuinely exists in
# the repo with a real resource declared in it, exactly the ADR's own
# documented false-positive shape ("a repo whose top-level directory is
# literally terraform-aws-modules").
#
# The resource below is therefore addressed as a plain root-module resource
# (`aws_security_group.vpc_endpoints`) by the config-vs-state drift loader,
# not `module.vpc.aws_security_group.vpc_endpoints`. Terraform state (this
# scope's side of testdata/cassettes/terraformstate/supply-chain-demo.json)
# carries the CORRECT module-prefixed address, so the mismatch produces the
# exact spurious pair go/internal/correlation/drift/tfconfigstate/doc.go
# describes: a spurious added_in_config for this wrongly-addressed resource,
# and a spurious added_in_state for the real module.vpc-prefixed resource.
# BuildCandidates attaches a terraform_module_resolution_confidence evidence
# atom (value "external_registry") to the added_in_config finding, and the
# reducer writer downgrades that finding's outcome from "exact" to "derived"
# -- proving issue #5572 end to end through the live golden-corpus gate, not
# only in go/internal/storage/postgres/tfstate_drift_evidence_module_confidence_test.go's
# unit and loader-integration tests.
#
# depth_exceeded (issue #5572's OTHER documented cause) deliberately has no
# golden-corpus fixture: it requires an 11-level-deep module chain to exceed
# maxModulePrefixDepth, a fixture heavy enough for a rare production shape
# that unit/integration coverage already proves precisely (including the
# depth-comparison fix itself). See
# docs/internal/evidence/5572-drift-derived-outcome-module-resolution-confidence.md
# for that explicit scoping decision.
resource "aws_security_group" "vpc_endpoints" {
  name        = "vpc-endpoints-sg"
  description = "Security group for VPC interface endpoints"
  vpc_id      = "vpc-0123456789abcdef0"
}
