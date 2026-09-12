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
- Test files -- this package's own tests, all moved in verbatim from root
  (see Move evidence).

## Move evidence

The family moved here from the query root (`query_playbook.go`,
`query_playbook_catalog.go`, `query_playbook_catalog_demo.go`,
`query_playbook_catalog_second_wave.go`, `query_playbook_catalog_third_wave.go`,
`query_playbook_handler.go`, `query_playbook_validate.go`, #6642); the
non-test files are destuttered (`query_playbook.go` -> `definition.go` since
its dominant content is the `Definition` type and its `Resolve`/param-binding
behavior; `query_playbook_catalog.go` -> `catalog.go`;
`query_playbook_catalog_demo.go` -> `catalog_demo.go`;
`query_playbook_catalog_second_wave.go` -> `catalog_second_wave.go`;
`query_playbook_catalog_third_wave.go` -> `catalog_third_wave.go`;
`query_playbook_handler.go` -> `handler.go`;
`query_playbook_validate.go` -> `validate.go`) and every `Playbook`- or
`QueryPlaybook`-prefixed exported identifier loses the stutter
(`QueryPlaybookHandler` -> `Handler`, `QueryPlaybook` -> `Definition`,
`PlaybookStep` -> `Step`, `PlaybookParam` -> `Param`, `PlaybookInput` ->
`Input`, `PlaybookInputType` -> `InputType`, `PlaybookInputIdentifier`/
`PlaybookInputString` -> `InputIdentifier`/`InputString`,
`PlaybookParamSource` -> `ParamSource` and its four `PlaybookParamFrom*`/
`PlaybookParamConst*` constants -> `ParamFromInput`/`ParamConst*`,
`PlaybookFailureMode` -> `FailureMode`, `PlaybookDrilldown` -> `Drilldown`,
`PlaybookVersionRef` -> `VersionRef`, `PlaybookCatalog` -> `Catalog`,
`PlaybookCatalogVersions` -> `CatalogVersions`, `PlaybookToolNames` ->
`ToolNames`, `LookupPlaybook` -> `Lookup`); `ResolvedPlaybook` and
`ResolvedCall` do not lead with the package word and keep their names.
The formerly root-unexported helpers `boolParam`, `constStringParam`,
`inputParam`, `limitParam`, `resolveParams`, and `isRawCypherTool` are
exported at their declaration here (`BoolParam`, `ConstStringParam`,
`InputParam`, `LimitParam`, `ResolveParams`, `IsRawCypherTool`) because the
staying `investigation_workflow_catalog.go`/`investigation_workflow.go` are
the callers that need them; `PlaybookParam.validateSingleSource` is exported
as `Param.ValidateSingleSource` for the same reason (`investigation_workflow.go`
calls it once). Every other unexported name (`source`, `rawCypherTools`,
`knownTruthClass`, `validateInputs`, `validate`, `validateParams`, and every
catalog-builder function) keeps its spelling. Root keeps every pre-move
exported spelling through a stanza in the new `query_playbook_alias.go`: the
`QueryPlaybookHandler`/`QueryPlaybook`/`PlaybookInputType`/`PlaybookInput`/
`PlaybookParamSource`/`PlaybookParam`/`PlaybookDrilldown`/`PlaybookStep`/
`PlaybookFailureMode`/`PlaybookVersionRef`/`ResolvedCall`/`ResolvedPlaybook`
type aliases, the `PlaybookInputString`/`PlaybookInputIdentifier` and four
`PlaybookParamFrom*`/`PlaybookParamConst*` const aliases, and the
`PlaybookCatalog`/`PlaybookCatalogVersions`/`PlaybookToolNames`/
`LookupPlaybook` forwarders. Their real callers are
`internal/answerquality/report_score.go` and
`internal/serviceintel/suggestions.go` (`LookupPlaybook`),
`internal/cli/hosted/onboard.go` (`PlaybookCatalog`),
`internal/demospec/manifest_test.go` (`PlaybookCatalog`, test-only) and
`internal/mcp/query_playbook_registry_test.go` (`PlaybookToolNames`);
`go/internal/mcp`'s `answer_parity_gen2_test.go` and
`demo_playbook_parity_test.go` also call several of them from test code.
`PlaybookCatalogVersions` has no caller outside this family today and is kept
so the alias stanza carries the whole enumeration. cmd/api's and
cmd/mcp-server's `wiring_router.go`, and the `apirecording`/`mcpreplay`
replay tests, construct `QueryPlaybookHandler` directly -- they do not call
any of these four forwarders. The unexported `boolParam`/`constStringParam`/
`inputParam`/`limitParam`/`resolveParams`/`isRawCypherTool` forwarders are
for the staying `investigation_workflow` family.
`investigation_workflow.go`'s one call to the now-exported method
(`param.validateSingleSource` ->
`param.ValidateSingleSource`) is the only edit outside the seven moved files
and the alias file: a type alias carries a renamed method's new exported
spelling automatically, but a lowercase method on a type declared in another
package is not reachable, so the one call site had to spell the exported
name.

Capability registration: contrary to the original move brief's
`rg -n -i 'playbook' go/internal/query/contract_*.go` verification (empty),
the Part C leaf `query/contract` (where #6687 moved those root
`contract_*.go` files; off-limits to this move) DOES carry a
`CapabilityQueryPlaybooks` row in `capability_matrix.go`'s
`baseCapabilityMatrix`, registered into `querycontract`'s shared capability
registry by that leaf's `init()`. `Handler.truth` calls
`querycontract.BuildTruthEnvelope`, which panics
(`query capability "query.playbooks" missing from capability matrix`) unless
that capability is registered first, so that `init()` -- not run by this
package's own test binary, which imports neither root (a cycle through
`query_playbook_alias.go`) nor `query/contract` -- had to be replaced by this package's own
registration for its tests to exercise the same gate production does. This
package declares its own `Capability` constant
(`Capability = "query.playbooks"`, byte-identical to root's literal) in
`handler.go` and its own capability-support contract (`Support`, in
`capabilities.go`, modelled on `workitem/capability.go`), registered by
`main_test.go`'s `TestMain` via `querycontract.RegisterCapabilities`. Root's
new `capability_lockstep_query_playbook_test.go` (package `query`, following
the shape of the since-retired work-item lockstep test that #6674 removed
when root's contract rows adopted the leaf constructors) asserts, field by
field, that
the `CapabilityQueryPlaybooks` row root reads through its `capabilityMatrix`
(the live `querycontract` registry the contract leaf fills) equals this
leaf's `Support()`, so the two copies of the same contract cannot drift apart
silently; a follow-up lane should point the contract leaf's row at `playbook.Support()`
directly, the way `contract/capability_matrix.go`'s
`repository.ContextOverviewCapability` entry already calls
`repository.ContextOverviewSupport()`. `Capability`'s value is not itself
threaded through an alias (unlike root's `work_item_alias.go`'s
`workItemEvidenceCapability` forwarder): root's `envelope_aliases.go` keeps its own
`CapabilityQueryPlaybooks = "query.playbooks"` literal (and
`contract/registry.go` its twin) completely unchanged by this move, since it already has no compile-time dependency on the moved
handler type.

Tests: all four family tests moved (`query_playbook_expansion_test.go`
-> `expansion_test.go`, `query_playbook_handler_test.go` -> `handler_test.go`,
`query_playbook_query_to_context_test.go` -> `query_to_context_test.go`,
`query_playbook_test.go` -> `definition_test.go`, matching `definition.go`'s
dominant content of catalog stability plus `Resolve`/`Validate` behavior);
every test kept its exact name. `expansion_test.go`, `query_to_context_test.go`,
and `definition_test.go` moved verbatim; `handler_test.go` did not -- its
three tests replace root's
`&APIRouter{Playbooks: &QueryPlaybookHandler{...}}; router.Mount(mux)` with
`&Handler{...}; handler.Mount(mux)` (the freshness/workitem-leaf test
precedent -- `APIRouter` is a root-only type this package cannot reach), a
harness change that mounts the identical two routes on a bare
`http.ServeMux` without going through root's top-level router. That harness
change also moved the assertion target: the three envelope-capability checks
(`handler_test.go:42,92,132`) now compare against this leaf's own
`Capability` constant instead of root's `CapabilityQueryPlaybooks` -- the two
are byte-identical strings (see the Capability registration paragraph below),
but the test now asserts against the package's own declaration rather than
the root one.

No-Regression Evidence: the `go test ./internal/query/...` test-name union
(`-list '.*'` filtered to `^Test`) against a base test list captured from an ephemeral detached
worktree at `origin/main` `5ed665821` equals that base's 4836 names across
`./internal/query/...` (root plus every leaf, not root alone) plus exactly
`TestQueryPlaybookCapabilityLockstep` (the capability-lockstep test this
move's registration gap required), nothing else dropped or duplicated (4837
total). Scoped to just the root package on this head, `go test
./internal/query -list '.*' | rg '^Test' | sort -u | wc -l` gives 2601 Test
names, and the same command against `./internal/query/playbook` gives 21;
their union is the base root list's 2621 Test names plus exactly
`TestQueryPlaybookCapabilityLockstep` (2622). A proof log that also counts
`Benchmark*` names reports nine `Benchmark*` entries as "added"; they all
exist at base root and are absent only from a Test-only baseline file, so
they are not new tests. The `internal/query` dirgate ledger row
(`scripts/lib/dirgate-grandfather.tsv`) moved from 282 non-test files
(digest `6192a683314447e9ee2b419858a07ab357fac5f95102e6208218528cc44a808a`) to
276 (digest
`31a76beb6dfd8b14189ef4091c092fca641e0d380d892eb2fc485783e40face7`; seven
files left root, one alias file and one lockstep test file arrived -- the
lockstep test does not count toward the non-test ledger); `bash
scripts/verify-dirgate.sh --all` passes with no exemption row for this new
leaf directory.

No-Observability-Change: this family emits no request span, unlike the
`queryspan.HandlerTracer`-seamed leaves (`freshness`, `language`, `incident`)
-- neither the pre-move root files nor this leaf call
`queryspan.HandlerTracer`, `startQueryHandlerSpan`, or any tracer. The truth
envelope's `capability`/`basis`/`level` metadata (`querycontract.BuildTruthEnvelope`)
is the only per-request signal this family emits, and it is unchanged by the
move: same capability string, same `TruthBasisRuntimeState` basis, same
`ProfileProduction` default.

## Related docs

- [docs/public/reference/query-playbooks.md](../../../../docs/public/reference/query-playbooks.md)
- [docs/public/reference/answer-packets.md](../../../../docs/public/reference/answer-packets.md)
- [docs/public/reference/investigation-workflows.md](../../../../docs/public/reference/investigation-workflows.md)
- [go/internal/query/read-models.md](../read-models.md)
