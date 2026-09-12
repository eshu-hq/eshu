# AGENTS.md — internal/query/iac guidance for LLM assistants

## Read first

1. `doc.go` — package contract, import boundary (must not import root package
   `query`), and the routes this family owns.
2. `../iac_alias.go` — root's compatibility shim: every pre-move exported
   type, the two Part C capability-string mirrors, and thin forwarders for
   the symbols staying root files and tests still need.
3. `capabilities.go` — the nine owned capability IDs' `Support()` contracts
   and the two Part C mirror constants; `../capability_lockstep_iac_test.go`
   pins both sides equal.
4. `../contract_capability_matrix.go` (Part C, do not edit) — the canonical
   registration of seven of this family's nine owned capability rows
   (lines 200-248); `../contract_replatforming_ownership.go:15` and
   `../contract_replatforming_rollups.go:14` (also Part C, do not edit)
   register the other two owned rows (`ReplatformingOwnershipCapability`,
   `ReplatformingRollupsCapability`) — this family reads those two strings
   through root's forwarding consts in `iac_alias.go`.
   `../contract_replatforming.go:21,28` (also Part C, do not edit) registers
   the two capabilities this family gates but does not own
   (`ReplatformingPlanReadinessCapability`, `ReplatformingSelectorInventoryCapability`).

## Invariants this package enforces

- **This package MUST NOT import root package `query`.** Root's
  `iac_alias.go` imports this package for the compatibility aliases; the
  reverse import would cycle. Any symbol this family needs that still lives
  only in root (an `openapi*`/`contract_*` file, or genuine production auth
  middleware) stays a root-only caller reached through the alias file, never
  an import into this package.
- **Capability gate before any read** — every handler calls
  `querycontract.CapabilityUnsupported` (or the family's own capability
  constant) before touching a store or `Graph`/`Content`. On failure, call
  `querycontract.WriteContractError`.
- **The two Part C capability mirrors must stay byte-identical strings** —
  `ReplatformingPlanReadinessCapability` and
  `ReplatformingSelectorInventoryCapability` in `capabilities.go` copy root's
  unexported `contract_replatforming.go` constants by value because a leaf
  package cannot see an unexported root identifier.
  `../capability_lockstep_iac_test.go` (root, package `query`) pins the
  mirror equal to root's constant; changing one string without the other
  desyncs the gate silently.
- **`InventoryStore` implementers live outside this package too** —
  `InventoryCandidate`, `InventorySearch`, and `InventorySummary` are
  exported so a root test double can implement `IaCInventoryStore` (the root
  alias). Do not re-privatize them without checking
  `resources_scope_auth_fakes_test.go` (root) first.
- **Port boundary** — handler structs hold `querycontract.GraphQuery`/
  `querycontract.ContentStore` interface fields, never a concrete Neo4j or SQL
  driver type. `PostgresIaCInventoryStore`, `PostgresIaCManagementStore`, and
  `PostgresIaCReachabilityStore` are this package's only adapters that touch
  `storage/postgres` directly.

## Common changes and how to scope them

- **Add a new route** → add the `mux.HandleFunc` call in `handler.go`'s
  `Mount`, add the capability constant and its `Support()` constructor in
  `capabilities.go`, register it in `main_test.go`'s `TestMain`, add the
  matching `openapi_paths_*.go` fragment in root (a Part C surface;
  coordinate with that lane), and update
  `docs/public/reference/http-api.md`. Run `go test ./internal/query/iac/...
  ./cmd/api ./internal/mcp -count=1` and
  `bash scripts/verify-route-coverage.sh`.
- **Rename or move a symbol this move exported only for a root
  forwarder/test double** (see README.md's Naming section for the list) →
  update the matching forwarder in `../iac_alias.go` or the matching fixture
  in `../resources_scope_auth_fakes_test.go` /
  `../replatforming_management_store_fake_test.go` /
  `../replatforming_rollups_handler_test.go` in the same change; a rename
  here without the root counterpart breaks the root build.
- **Add or change a capability this family owns** → add the ID constant near
  the route it gates, add a `Support()` constructor in `capabilities.go`,
  register it in `main_test.go`, and add the matching literal row to root's
  `contract_capability_matrix.go` (Part C) plus
  `specs/capability-matrix.v1.yaml` or its per-capability fragment; run
  `go test ./internal/query/iac/... -count=1` (the family's own capability
  gate) and `go test ./internal/query/ -run CapabilityLockstep -count=1`
  (the cross-package pin).

## Failure modes and how to debug

- Symptom: `go test ./internal/query/iac` fails every handler test with
  `unsupported_capability` → likely cause: a capability this family owns
  isn't registered in this test binary → check `main_test.go`'s `TestMain`
  registers it; production registers the same capability through root's
  `contract_capability_matrix.go`, which never links into this package's own
  test binary (see `main_test.go`'s file doc comment).
- Symptom: root package `query` fails to build with `undefined: <SomeName>`
  after a rename here → likely cause: `../iac_alias.go` (or a root test
  fixture file) still spells the pre-rename name → grep root for the old
  name and update every forwarder/fixture in the same change.
- Symptom: `internal/queryplan`'s `TestHotCypherManifestCoversEveryProductionQueryCall`
  fails with a stale/unregistered callsite naming a file in this package →
  likely cause: a hot-path query method moved or was renamed here without
  re-pinning `internal/queryplan/testdata/query-source-coverage.yaml`'s
  matching row (path, symbol, and `source_sha256` -- see README.md's Move
  evidence for the pattern this move followed).

## What NOT to change without an ADR

- The two Part C capability-string mirrors' values
  (`ReplatformingPlanReadinessCapability`, `ReplatformingSelectorInventoryCapability`)
  — they must always equal root's `contract_replatforming.go` constants;
  changing either side alone silently desyncs the capability gate.
- `Handler`'s exported field names (`Content`, `Reachability`, `Management`,
  `Inventory`, `Graph`, `Profile`) — `cmd/api/wiring_router.go` and
  `cmd/mcp-server/wiring_iac.go` construct this struct with keyed literals
  through the root alias; a field rename breaks both without a compiler error
  pointing here (the field name mismatch surfaces in `cmd/api`/`cmd/mcp-server`).
