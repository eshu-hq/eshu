# CloudRuntimeDriftReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. W8b tier-1 CloudRuntimeDrift family core pin, per
the arbiter W8b ruling (APPROVED-WITH-CONDITIONS): ONE narrow v0 pin,
pin-first only, no termination, no tracker ticks. Minted core-side; the
satellite records this pin in a follow-up. A satellite PR consuming an
unminted pin is invalid. Arbiter review of this pin precedes satellite
termination of the drift legs.

## Shape

Version: `CloudRuntimeDriftReadAdmissionContractVersion = "v0"`
(go/internal/query/cloud_runtime_drift_read_admission.go).

Ceiling 2 re-declared (never embedded) methods — the bounded
provider-aggregated active-finding page plus the total active count for
the same bounded filters:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `ListActiveMultiCloudRuntimeDriftFindings(context.Context, MultiCloudRuntimeDriftFilter) ([]MultiCloudRuntimeDriftFindingRow, error)` | `MultiCloudRuntimeDriftStore` (go/internal/query/cloud_runtime_drift.go) | `POST /api/v0/cloud/runtime-drift/findings` (bounded readback page); `GET /api/v0/investigations/drift/packet` (packet build) |
| 2 | `CountActiveMultiCloudRuntimeDriftFindings(context.Context, MultiCloudRuntimeDriftFilter) (int, error)` | `MultiCloudRuntimeDriftStore` | `POST /api/v0/cloud/runtime-drift/findings` (truncation total) |

There is deliberately no `WithReadStore` constructor on this pin: the
satellite keeps building the Postgres adapter the way `cmd/api` wiring
does today (`NewPostgresMultiCloudRuntimeDriftStoreWithReadStore` over
the request-scoped guarded reader), and the pin freezes only the read
shape the handlers may call through. The production satisfier assertion
in the pin file (`var _ CloudRuntimeDriftReadAdmission =
(*PostgresMultiCloudRuntimeDriftStore)(nil)`) proves the wired adapter
satisfies the pin.

Enforcement (all in go/internal/query):

- `cloud_runtime_drift_read_admission_contract_test.go` — frozen
  method-set test plus version pin
  (`TestCloudRuntimeDriftReadAdmissionContractIsVersioned`), seal
  (`TestCloudRuntimeDriftReadAdmissionWireShapesAreSealed` — exactly 2
  methods, no graph/content/snapshot/ledger/management surface), and
  the seeded-drift RED half
  (`TestCloudRuntimeDriftReadAdmissionSeededDriftIsDetected` — the
  planted `RunSingle` widening must not match the frozen set).
- `cloud_runtime_drift_read_admission_routes_test.go` — handler tests
  for both routes through the real `CloudRuntimeDriftHandler` mux with
  a pin-half double (findings readback records exactly the
  count-plus-list pair, the packet leg records exactly the list read),
  plus the route/spec-fragment parity test (every pinned path in the
  OpenAPI cloud and evidence fragments and served by `Mount`).

## Census (per route per method)

| Method | `POST /api/v0/cloud/runtime-drift/findings` | `GET /api/v0/investigations/drift/packet` |
|---|---|---|
| `ListActiveMultiCloudRuntimeDriftFindings` | bounded readback page | packet build (only store read on the leg) |
| `CountActiveMultiCloudRuntimeDriftFindings` | truncation total | — (no total on the packet; OUT by construction) |

Reachability notes, recorded not silently assumed: the packet leg
short-circuits before any store read on a scoped out-of-grant scope
(refusal packet, no store call) and on a missing scope or unknown
provider (contract error, no store call); the count read is reachable
only from the findings leg. No `Run`/`RunSingle` graph read, no content
read, and no second store port is reachable from either route — both
legs go through the single `Store` field on the one `Mount`.

Every other `MultiCloud*` shape is OUT: there is no third method on the
store, so there is nothing further to exclude on this port.

## Explicitly OUT (not widened by this pin)

- `GraphQuery.Run` / `RunSingle` — no drift route issues a graph read;
  inventory, selector, and reachability legs belong to the IaC pin.
- `ContentStore` file/entity reads — no drift route reads content; the
  dead-route content fallback rides the frozen CoreReadContracts.
- AWS-specific row extras (`MatchedTerraformConfigFile`,
  `MatchedTerraformModulePath`, service/environment candidates,
  dependency paths) — structurally unreachable on any real fact; the
  pin projects exactly the provider-neutral row.
- Grant checks, fixed failure messages, packet bounds, the truth
  envelope, and `QueryProfile` (handler behavior, not port shape).
- Ledger / stored-summary / snapshot surface (none on this family;
  sealed out regardless).
- TerraformDrift #10442 and IaC #10455 are NOT coverage for either
  route here: citing them (or the drift pin's mcp37 same-handler
  sentence) as coverage is void — neither re-declares the
  provider-aggregated `MultiCloudRuntimeDriftStore` surface.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.

## Satellite consumption

Depend on the shape, not a new constructor: serve the two routes'
reads through the existing Postgres adapter built the way `cmd/api`
wiring builds it today
(`NewPostgresMultiCloudRuntimeDriftStoreWithReadStore`), the same
concrete store `CloudRuntimeDriftHandler.Store` holds. A satellite PR
consuming this pin for any other backend is invalid without arbiter
review. Termination follows the W8b order: whole-family at once (both
routes together, never partial), with byte-parity plus latency proof,
and only after this pin's arbiter review lands.
