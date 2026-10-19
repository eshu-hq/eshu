# Fixture for the incident_routing_evidence family Odù (#6228).
#
# go/internal/parser/hcl/pagerduty_declarations.go recognizes a `module` block
# whose `source` contains "pagerduty-service" (isSupportedPagerDutyModuleSource)
# and records it as a pagerduty_declarations row, which
# entityBucketsFromParsed projects to a PagerDutyDeclaration content entity
# whose metadata the incident-routing guard decodes through the production
# reducer/incident seam. Both literal inputs below resolve "literal", so the
# declared candidate stays exact rather than derived; a second module block
# would mark the shared service name duplicate and flip the fixture ambiguous,
# so the negative control lives in the Odù facts (a tombstoned applied fact)
# instead of here.
module "checkout" {
  source            = "terraform-modules/pagerduty-service/pagerduty"
  name              = "Checkout API"
  escalation_policy = "PEP1"
}
