# #5954: real on-call service declaration coverage for PagerDutyDeclaration.
# go/internal/parser/hcl/pagerduty_declarations.go recognizes a `module` block
# whose `source` contains "pagerduty-service" (isSupportedPagerDutyModuleSource)
# and records it as a pagerduty_declarations row, which
# go/internal/projector/canonical.go's entityTypeLabelMap projects as a
# PagerDutyDeclaration node. Before this fixture, the corpus had zero blocks
# matching that shape, so the label had no live end-to-end proof.
module "orders_pagerduty_service" {
  source  = "terraform-modules/pagerduty-service/pagerduty"
  version = "2.4.0"

  name                    = "orders-api"
  description             = "On-call routing for the orders API service"
  escalation_policy       = "orders-escalation-policy"
  incident_urgency        = "high"
  acknowledgement_timeout = "1800"
  auto_resolve_timeout    = "14400"
}
