# Playbook — Agent Instructions

Scope: `go/internal/query/playbook/` (package `playbook`).

## Ownership

This leaf owns the deterministic query-playbook catalog and resolver
(#6642): `definition.go` (`Definition`, `Resolve`, the catalog-listing
helpers), `validate.go` (`Validate` and the no-raw-Cypher rule), `catalog.go`
plus `catalog_demo.go`/`catalog_second_wave.go`/`catalog_third_wave.go` (the
versioned catalog), `handler.go` (`Handler` and its two routes),
`capabilities.go` (this family's capability contract), `main_test.go` (the
capability-registration `TestMain`), plus this package's own tests, all
moved in verbatim from root -- see README.md's Move evidence.

## Invariants

- MUST NOT import root package `query` -- root would import this package
  back for the compatibility aliases in `query_playbook_alias.go`, cycling.
  Reach root-only helpers through `querycontract` (profiles, envelopes,
  error/response contracts, the `AnswerTruthClass` taxonomy, capability
  registration); if it does not have what you need, it does not belong here
  -- ask before adding a new shared home.
- `Capability` MUST stay `"query.playbooks"` -- byte-identical to root's
  `envelope_aliases.go` `CapabilityQueryPlaybooks` constant, its twin in
  `contract/registry.go`, and the row it feeds into the Part C leaf's
  `contract/capability_matrix.go` `baseCapabilityMatrix`.
  `capability_lockstep_query_playbook_test.go` (root, package `query`) pins
  both the `Capability`/`CapabilityQueryPlaybooks` string equality and every
  `Support()` ceiling field by field, since the contract leaf's row is still a
  literal copy, not a call into `Support()`. `go/internal/mcp`'s
  `TestDemoPlaybookHTTPAndMCPParity` and the `apirecording` golden
  (`TestQueryPlaybookRecordingMatchesGolden`) are the production-shaped
  catchers: both mount the real handler and panic
  (`query capability "..." missing from capability matrix`) on a one-sided
  `Capability` edit, so a production regression is still caught even though
  the lockstep test only compares the two in-repo declarations against each
  other.
- `main_test.go`'s `TestMain` registering `Capability` through `Support()` is
  NOT redundant with the contract leaf's `init()` registration in
  `contract/capability_matrix.go`: this package's own test binary never
  links root (an import would cycle through `query_playbook_alias.go`) or
  `query/contract`, so that `init()` never runs here. Delete `TestMain` and every handler test
  in this package panics on `BuildTruthEnvelope`'s
  `query capability "query.playbooks" missing from capability matrix` --
  not because the handler is broken, but because no capability was ever
  registered for it to check against. Follow the `workitem`/`freshness`
  precedent, not a hand-copied literal: call `Support()`, never re-type its
  field values into a second literal.
- This family declares NO span or tracer (unlike `freshness`, `language`, or
  `incident`'s `queryspan.HandlerTracer` seam) -- neither before nor after
  this move. Do not add one without checking whether root's telemetry
  contract (`docs/public/observability/telemetry-coverage.md`) expects one;
  `BuildTruthEnvelope`'s capability/basis/level metadata is this family's
  only per-request signal.
- `investigation_workflow.go`/`investigation_workflow_catalog.go` (package
  `query`, root, staying for a later #6642 lane) reach `Input`,
  `InputIdentifier`, `InputString`, `Param`, `Step`, and the unexported
  `boolParam`/`constStringParam`/`inputParam`/`limitParam`/`resolveParams`/
  `isRawCypherTool` forwarders through `query_playbook_alias.go`'s type
  aliases and thin forwarders. `Param.ValidateSingleSource` is exported at
  its declaration (dropping what would otherwise be unexported,
  root-package-private naming) because `investigation_workflow.go`'s
  `validateWorkflowCall` is the caller that needs it -- a type alias carries
  a renamed method automatically, but a lowercase method on a type declared
  in another package is unreachable, so that one root call site had to
  spell `param.ValidateSingleSource` instead of `param.validateSingleSource`.
  Do not rename or unexport it without checking that call site.
- `Param.source` (unexported) and `rawCypherTools` (unexported) have no
  caller outside this package and stay unexported; do not export them
  "for symmetry" with the six forwarded helpers above -- export only what a
  staying root caller actually needs.

## Naming

`docs/internal/naming.md` is law: no `playbook_` file prefixes, no
`playbook/query_playbook.go`, exported identifiers lose the family stutter
except where the forwarder rule above names a specific root caller.
`ResolvedPlaybook`/`ResolvedCall` do not lead with the package word
(`Resolved`, not `Playbook`) and keep their pre-move spelling unchanged. The
root `query_playbook_alias.go` keeps every old exported spelling for staying
callers. `ResolvedPlaybook.PlaybookID` (naming-audit rule-4 exception,
matching `freshness/causality_report.go`'s `Transition.FreshnessHint`
precedent) is a struct field, not a top-level declaration, and it is the
`playbook_id` JSON wire-contract key this family's HTTP resolver response
and `go/internal/mcp`'s parity tests already decode -- renaming it would
break the wire contract for no naming benefit and is out of scope for the
rule-4 exported-identifier check per the move brief.
