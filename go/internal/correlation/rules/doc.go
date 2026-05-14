// Package rules defines the correlation rule-pack schema and the first-party
// rule packs shipped with Eshu.
//
// A `RulePack` carries a minimum admission confidence, a set of structural
// `EvidenceRequirement` entries, and a bounded list of `Rule` entries with
// kinds drawn from `RuleKind`. Every pack must pass `RulePack.Validate`
// before evaluation; the engine relies on the schema invariants enforced
// here (non-negative priority, bounded match count, required selectors)
// when ordering rules and counting matches. First-party packs include
// language, container, deployment, Terraform config/state drift, and AWS
// cloud-runtime drift rules.
package rules
