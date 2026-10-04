# Supply-chain query hub

## Purpose

Owns the `Handler` HTTP surface: nineteen routes over
reducer-owned supply-chain truth, served from Postgres read models and the
graph (eleven in `Mount`, plus a count/inventory pair per aggregate
family).

| Route | Handler |
|---|---|
| `GET /api/v0/supply-chain/vulnerability-scanner/contract` | `getVulnerabilityScannerReadContract` |
| `GET /api/v0/supply-chain/sbom-attestations/attachments` | `listSBOMAttachments` |
| `GET /api/v0/supply-chain/advisories` | `listAdvisoryCatalog` (reads `advisory.`) |
| `GET /api/v0/supply-chain/advisories/evidence` | `listAdvisoryEvidence` (reads `advisory.`) |
| `GET /api/v0/supply-chain/vulnerabilities/{advisory_id}` | `getVulnerabilityDetail` |
| `GET /api/v0/supply-chain/impact/findings` | `listImpactFindings` (reads `impact.`) |
| `GET /api/v0/supply-chain/impact/explain` | `explainImpact` (reads `impact.`) |
| `POST /api/v0/supply-chain/impact/suppressions` | `createVulnerabilitySuppression` |
| `GET /api/v0/investigations/supply-chain/impact/packet` | `getImpactPacket` (composes via the injected packet responder) |
| `GET /api/v0/supply-chain/container-images/identities` | `listContainerImageIdentities` |
| `GET /api/v0/supply-chain/security-alerts/reconciliations` | `listSecurityAlertReconciliations` |
| impact aggregate routes | `supplyChainImpactAggregateRoutes` (count + inventory) |
| security-alert aggregate routes | `securityAlertReconciliationAggregateRoutes` |
| container-image aggregate routes | `containerImageIdentityAggregateRoutes` |
| sbom-attachment aggregate routes | `sbomAttestationAttachmentAggregateRoutes` |

Three runtime-evidence probes enrich impact findings before they are
served: the cloud probe (`cloud_runtime_probe.go`),
the Kubernetes probe (`kubernetes_runtime_probe.go`
plus the fairness fan-out in
`kubernetes_runtime_probe_fair.go`), and the runtime
context applier (`runtime_context_probe.go`). All
three promote a finding to `runtime_confirmed` only on current,
caller-authorized evidence; a nil inventory store disables its tier.

## Ownership boundary

This package owns the handler, the store ports, and the row/filter/page
values crossing them for the container-image, SBOM attestation, security
alert, and cloud-runtime families, plus the suppression mutation port,
the scanner contract shapes, the repository selectors, and the probe
budgets. The advisory and impact read models stay in the `advisory/` and
`impact/` subpackages; the Postgres store *implementations* stay in root
package `query` (shared with entity and incident-context reads) and
satisfy the hub ports through root's compatibility aliases.

Root package `query` keeps the capability matrix
(`contract_supply_chain.go`), the cross-cutting auth and
graph-error sweeps, the `factschema_decode_supplychain.go` decoder, and
the lane-B packet envelope (`investigation_packet_*.go`). Root owns the
router and always links into the production binary, so capability
registration and the `SupplyChainHandler` compatibility alias
(`compat_supply_chain.go`) live there. `cmd/api` and `cmd/mcp-server`
construct the handler as `query.SupplyChainHandler` exactly as before. The
OpenAPI path fragments for this family live in
`go/internal/query/openapi/paths/supply/chain/`, assembled by
`openapi.Spec()`.

## Exported surface

Every export names a staying root caller; see `AGENTS.md` for the
per-symbol list. In brief: the handler and its result/request/response
types; the store ports and the filter/row/page/count values crossing
them; the capability, limit, and probe-budget constants the staying
contract matrix, staying stores, and staying tests read; the shared
seams staying root files reuse (`UniqueSortedNonEmpty`,
`SecurityAlertRepositoryScopeIDs`,
`CloudRuntimeProbePerDigestLimit`); and the
`ImpactPacketResponder` port root implements from the lane-B
packet envelope. See `doc.go` for the godoc-rendered contract.

## Dependencies

Internal packages, all of them leaves that never import root package
`query`:

- `internal/query/querycontract` — envelopes, capabilities, profiles,
  row-value decoders, repository access filter, collector readiness.
- `internal/query/auth` — auth context bounds and normalization.
- `internal/query/selector` — repository-selector resolution.
- `internal/query/tracing` — handler span plumbing.
- `internal/query/supply/chain/advisory`, `.../impact` — the advisory and
  impact read models.

Plus `internal/scope` (collector kinds), `internal/telemetry` (span
names), `internal/environment`, `internal/facts`, and
`sdk/go/factschema` (typed suppression decode). The `attachCollectorListReadiness`
copy, the tracer, and the graph-configured predicate are family-local
copies of trivial root helpers that cannot cross the package boundary;
each carries a provenance comment naming its root source.

## Telemetry

Handler routes open spans named in `internal/telemetry`
(`SpanQueryContainerImageIdentities`, `SpanQueryVulnerabilitySuppressionMutation`,
and the impact/advisory span names the moved handlers already used).
The cloud and Kubernetes probes open child spans carrying digest counts,
candidate counts, and authorization outcomes, so an operator can read
exactly why a finding did or did not promote to `runtime_confirmed`.
Span names, capability strings, and attribute keys are unchanged by the
move.

`GET /api/v0/supply-chain/impact/findings` also logs one bounded event per
backing read (`query_timing.go`, issue #7007): `supply_chain_query.stage_started`
and `supply_chain_query.stage_completed` for the stages `impact_findings_query`,
`cloud_runtime_evidence`, `kubernetes_runtime_evidence`, `runtime_context`, and
`readiness_snapshot`. Every completion carries a boolean `error` attribute,
including `impact_findings_query`, so a failed read and an empty page no longer
log identically (#7546).

The `impact_findings_query` completion also carries `reader_borrow_seconds`,
`reader_identity_seconds`, `reader_replay_seconds`, and `business_query_seconds`
(#7545). The handler wraps only the findings read in `db.WithStageTimings`, and
the guarded reader adds each stage it pays for; each value is the SUM of that
stage across the reader operations inside the findings read, and the four
attributes are present only when the guarded reader recorded a stage. The other
stages, the cloud-runtime probe, and the readiness read do not carry them.
`business_query_seconds` covers only the database call that starts the query;
row streaming, scanning, and decoding happen afterward and are not timed, so the
four sums can sit well below the stage's `duration_seconds`. The values are
sums, which could exceed wall time only if reader operations ran concurrently in
one scope; the findings read runs them one after another, so they cannot.

A handler-owned HTTP 500 on the route additionally emits ONE ERROR-level
`supply_chain_query.stage_failed` event, because `querycontract.WriteError`
never logs. It carries `operation`, `stage`, `repo_id`, `duration_seconds`, and:

- `error`: the error text, cut to 256 bytes on a UTF-8 boundary. The guarded
  PostgreSQL reader's errors already carry a fixed site string such as
  `PostgreSQL reader connection unavailable`. For the findings read this is the
  same text the response body returns; the cloud-runtime, Kubernetes-runtime and
  runtime-context branches return a fixed body, so there the log carries the
  underlying error text, still cut to 256 bytes.
- `error_site`, a closed set from the error chain: `reader_stale`,
  `reader_unavailable`, `other`. The writer-side and topology sentinels
  (`ErrWriterUnavailable`, `ErrMissingCheckpoint`, `ErrWrongTopology`) live in
  `internal/runtime/postgres`, which the query layer must not import, so they
  classify as `other`.
- `error_cause`, a closed set that never contains error text:
  `deadline_exceeded`, `canceled`, `conn_done`, `eof`, `conn_refused`,
  `conn_reset`, `net_timeout`, `sqlstate_<two-character class>`, `unknown`.
  The SQLSTATE class comes from an `interface{ SQLState() string }` found with
  `errors.As`, and only a well-formed two-character class is kept.

The same branches record the error on the handler span (`RecordError`) and set
its status to Error with the fixed description
`supply-chain impact findings stage failed`. The graph and
reader-fence verdicts that `querycontract.WriteGraphReadError` maps to 503/504
are not handler-owned 500s and are not logged by this event. The unchanged wire
contract (status codes and response bodies) is pinned by
`TestListImpactFindingsLogsFailedStageOnHandlerOwned500`, and the silence and
unchanged status of the mapped verdicts by
`TestListImpactFindingsMappedVerdictsStaySilentAndUnchanged`.

Every backing read on the route calls `querycontract.WriteGraphReadError`
BEFORE `failStage`, including the findings read and the cloud-runtime probe
read (#7548): a stale guarded PostgreSQL reader (`db.ErrReaderStale`), or one
whose connection acquisition or identity check timed out inside the replay
window (`db.ErrReaderUnavailable` joined with `context.DeadlineExceeded`),
answers the retryable `503` `backend_unavailable` with `Retry-After` and no
`stage_failed` line. A reader failure that is not a timeout (a bare
`db.ErrReaderUnavailable`, a refused or reset connection) is not transient and
still answers `500` with the `stage_failed` line, where `error_site` and
`error_cause` classify it. The readiness read is unchanged: its error serves a
`readiness_unavailable` envelope, not a `500`. Pinned by
`TestListImpactFindingsReaderTimeoutAnswersRetryable503`.

## Move evidence (#6060)

This package was created by moving twenty-five files out of root package
`query` (`git mv`, no logic changes). The two assertions below are
structural rather than promissory — each names what a reader can check.

No-Regression Evidence: the move is a package relocation, not a rewrite.
`git diff -M --find-renames` pairs each file with its root predecessor;
the only statement-level changes are the `package` clause, the
`querycontract`/`auth`/`selector` qualification of helpers
root forwards to the identical functions, the export renames listed in
`AGENTS.md`, the family-local copies documented above, and the packet
responder seam (the route's request parsing, store reads, and response
bytes are unchanged; composition still runs the same lane-B builder).
The moved test suite (`go test ./internal/query/supply/chain/`) pins
handler, scope, probe, and readiness behavior from inside the package,
and the staying root suite (`go test ./internal/query/...`) pins the
routes, contract matrix, and packet parity through the aliases.

No-Observability-Change: span names, capability strings, attribute keys,
and the tracer seed (`tracing.HandlerTracer`) are unchanged; only the
package qualifier moved.

## Gotchas / invariants

- Do not import root package `query`. Root's
  `compat_supply_chain.go` already imports this package, so the
  reverse import cycles.
- Capabilities are registered in ROOT (`contract_supply_chain.go`), not
  here — root owns the router and always links into production.
- Every list route needs a bounded scope or an explicit limit; the
  handler rejects anchorless reads before any store runs.
- The probes never surface unauthorized or stale evidence: nil inventory
  disables the tier, and eligibility runs inside the bound, not after.
- The packet route never touches lane-B packet types directly; it
  composes through `ImpactPacketResponder`, which root
  injects. If lane-B moves the envelope to a leaf, this seam collapses
  back to direct calls.

## Related docs

- [HTTP API Reference](../../../../docs/public/reference/http-api.md)
- [Telemetry](../../../../docs/public/reference/telemetry/index.md)
- [Architecture](../../../../docs/public/architecture.md)
