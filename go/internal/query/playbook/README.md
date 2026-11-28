# Playbook

## Purpose

The deterministic query-playbook catalog and resolver (#6642): the catalog
list route (`GET /api/v0/query-playbooks`) and the resolver route
(`POST /api/v0/query-playbooks/resolve`). A playbook is a machine-readable,
bounded description of a common starter-prompt or cookbook workflow -- it
names the ordered first-class tool calls, their bounded parameters, the
expected truth and evidence per step, optional drilldowns, and the declared
failure modes -- so an agent surface can reach an
[answer packet](../../../../docs/public/reference/answer-packets.md) without
guessing a tool, skipping a bound, or inventing a parameter. See
[docs/public/reference/query-playbooks.md](../../../../docs/public/reference/query-playbooks.md)
for the external contract.

## Naming

The package is named `playbook` because it holds the playbook catalog,
handler, and validation; the sibling `investigation_workflow` family that
consumes several of its helpers (the guided, missing-evidence-driven catalog)
stays in root for now and moves onto this leaf in a later #6642 lane -- the
#6642 table lists no separate playbook row because it counted these files
under the investigation-family total. `QueryPlaybook`, the definition type,
is renamed `Definition` at its declaration (rule 4: an exported identifier
must not lead with the package word `Playbook`) -- it is a versioned
description a caller resolves, so `Definition` is the honest noun for what
the type actually is, and it keeps `playbook.Definition` free of any repeat
of "playbook".

## Ownership boundary

Owns `Handler`, its two routes' HTTP dispatch, `Definition` and its
`Resolve`/`Validate` methods, the versioned catalog (`Catalog`,
`CatalogVersions`, `Lookup`, `ToolNames`), the parameter-binding helpers
(`InputParam`, `LimitParam`, `ConstStringParam`, `BoolParam`, `ResolveParams`),
and the raw-Cypher-tool rejection (`IsRawCypherTool`). Does not own the
capability registry or truth-envelope contract (`querycontract`) or the
answer-packet truth-class taxonomy (`querycontract.AnswerTruthClass`) --
those are separate homes this package calls into. Does not own
`investigation_workflow.go`/`investigation_workflow_catalog.go` (package
`query`): that family stays in root for now and reaches this package's
parameter-binding helpers and shared types (`Input`, `InputIdentifier`,
`InputString`, `Param`, `Step`, and `Param.ValidateSingleSource`, exported
at its declaration because `investigation_workflow.go` is the caller that
needs it) through `query_playbook_alias.go`'s type aliases and unexported
forwarders. A later PR moves `investigation_workflow` onto this leaf and
drops those forwarders.

## Layout

- `definition.go` -- `Definition` (the playbook definition type), `Input`,
  `InputType` and its two constants, `Param`, `ParamSource` and its four
  constants (plus `Param.source`), `Drilldown`, `Step`, `FailureMode`,
  `VersionRef`, `ResolvedCall`, `ResolvedPlaybook`, `Definition.Resolve`,
  `ResolveParams`, and the catalog-listing helpers `ToolNames`,
  `CatalogVersions`, `Lookup`. The four parameter-binding constructors
  (`InputParam`, `LimitParam`, `ConstStringParam`, `BoolParam`) live here
  beside the type they build.
- `validate.go` -- `rawCypherTools`, `IsRawCypherTool`, `Definition.Validate`
  and `Definition.validateInputs`, `Step.validate` and `Step.validateParams`,
  `Param.ValidateSingleSource`, and `knownTruthClass`.
- `catalog.go` -- `Catalog` (the versioned source of truth) and the first
  three playbooks (service story, repository code-topic, documentation
  truth) plus their param/failure-mode builders.
- `catalog_demo.go` -- the three demo first-five-questions playbooks
  (issue #4745): `demoDeploymentToCloudResourcePlaybook`,
  `demoDependencyCrossRepoPlaybook`, `demoObservabilityToWorkloadPlaybook`.
- `catalog_second_wave.go` -- the six second-wave playbooks (incident
  context, supply-chain impact, secrets/IAM posture, incremental freshness,
  hosted onboarding governance, change-surface source investigation).
- `catalog_third_wave.go` -- the four "query-to-context" playbooks that
  start from semantic search and bridge into bounded readbacks, plus their
  shared inputs/search-step/failure-modes builders.
- `handler.go` -- `Capability`, `Handler`, `Handler.Mount` and its two route
  handlers (`list`, `resolve`), the unexported `listResponse`/
  `resolveRequest`/`resolveResponse` wire types, and `Handler.truth`/
  `writeError`/`profile`.
- `capabilities.go` -- `Support`, this family's capability contract
  constructor (see AGENTS.md).
- `main_test.go` -- `TestMain`, registering this family's capability before
  any test runs (see AGENTS.md).
- Test files -- package tests (`definition_test.go`, `expansion_test.go`,
  `handler_test.go`, `query_to_context_test.go`).

## Related docs

- [docs/public/reference/query-playbooks.md](../../../../docs/public/reference/query-playbooks.md)
- [docs/public/reference/answer-packets.md](../../../../docs/public/reference/answer-packets.md)
- [docs/public/reference/investigation-workflows.md](../../../../docs/public/reference/investigation-workflows.md)
- [go/internal/query/read-models.md](../read-models.md)
