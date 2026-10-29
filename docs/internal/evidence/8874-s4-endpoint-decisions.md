# 8874 S4 endpoint decisions (endpoint vs retain-forever)

Refs #9510, part of #6707. Scope: Tier-3 CLI families that cannot populate
until a core endpoint exists (population-scope verdict, decisions 3-4).

Base: `origin/main` `73418463ed`. Method: closure census (`go list -deps`),
caller census (`git grep`), inventory classification
(`go/cmd/eshu/cli_extraction_inventory.md`), ownership map
(`docs/internal/design/eshu-ecosystem-repository-migration.md`).

## Decision table

| Family | Census | Decision |
| --- | --- | --- |
| `docs verify` | `cli/docs/verify.go` imports `doctruth` + `query`; `doctruth` non-CLI consumers: `query/service/story_supply_chain_image.go`, `semanticcode/emitter.go`, `semanticdocs/emitter.go`, `workflowimage/extract.go`, collector `document/media` + `repo/git/docs`; `doctruth` closure 21 internal packages; `verify` opens Postgres persistence and scans local files | Retain-forever in core. A remote endpoint cannot read the operator's local files without upload and cannot take a client Postgres DSN. Verdict NOT_CHECKED explicitly allows this outcome. |
| `scan` / `vuln-scan` local modes | `localsupervisor` (owner records, PIDs, sockets, child processes, embedded NornicDB); `vulnscan/localruntime.go` attaches to local owner, supervises processes | Retain-forever in core. Local process supervision and sockets are inherently local-machine operations; the named future route is `local-host/v1`, out of S4 scope. Remote index-and-wait paths are already thin (Tier-1). |
| `service` local profiles (+ `graph` sidecar) | `service.go` manages local owners (start/stop local API+MCP, embedded graph); inventory marks `graph` RETAIN (localsupervisor owns core schema/embedded Postgres/child processes) | Retain-forever in core. Owner lifecycle is local execution, not a query. |
| `component` local verbs (`conform`, `grants`, install/trust) | `cli/component/conform.go` runs `extensionconformance.Run` in-process (bounded extension execution); `grants.go` uses `componentcore.NewRegistry(home)` (local trust-state files) | No core endpoint: extension-host-bound. Server-side extension execution would violate the trust boundary; local grant files are not a core contract. Verbs stay in the CLI until the `eshu-extension-host` move owns them (migration plan: installation, trust checks, bounded execution move to `eshu-extension-host`). `inventory`/`diagnostics` are already thin (own envelope, remote fetch). |
| `investigation export` render | `RenderInvestigationPacket` pure over the supplied packet (encoding/json, html, sort, strings); packet types live in the 452-closure `query` root so the function cannot move | Endpoint: `POST /api/v0/investigation/packet/render`. Thin client sends the fetched packet JSON it already holds; no new truth source. |
| `service-report` compose | `serviceintel.Compose` pure over supplied sections (454-closure via `query`); `service-report` is an offline composer over captured files, EXTRACT per inventory | Endpoint: `POST /api/v0/service/report/compose`. Request carries the captured dossier/truth the CLI already parsed; server runs `FromServiceStory`/`FromSupplyChainInventory` + `Compose`. |
| `report` capture | `reportbundle.Capture` redacts/digests/validates client-supplied inputs (456-closure via `query`+`facts`, incl. `storage/postgres`); cannot move, cannot publish (redaction/composition logic forbidden in SDK) | Endpoint: `POST /api/v0/report/bundle/capture`. Request carries the target/params/note/envelope the CLI already holds; the CLI never fetched server-side. Credential-bearing notes are redacted before storage; the endpoint never logs bodies (metrics middleware records duration/errors only). Contract tests reuse the credential canary. |
| `report` validate | `reportbundle.Validate` pure over bundle bytes; same package closure blocks the move | Endpoint: `POST /api/v0/report/bundle/validate`. Request carries the bundle file bytes; 200 always carries the `{valid, message}` verdict. |
| `answerquality` scoring | `answerquality.Score` over captured evidence (456-closure via `query`+`serviceintel`); command is EXTRACT, offline scorer | Endpoint: `POST /api/v0/answer-quality/score`. Request carries the evidence file bytes the CLI already reads. |

## Wire rules for the five endpoints

- Pure functions of the request body: no graph, Postgres, collector, or
  queue reads. Zero construction dependencies; one `Mount` call in
  `go/cmd/api/wiring.go` beside `serviceintelhttp`.
- Additive only: no existing route, handler, or spec changes. New OpenAPI
  fragments under `openapi/paths/cli/`, concatenated in `spec.go`; the
  `http-api.md` Route Families row for stories links the new
  `cli-support-endpoints.md` sub-page (folded into the existing row — the
  hub sits exactly at its 450-line cap, so no new row); surface inventory
  regenerated.
- Bounded bodies (`MaxBytesReader`): 1 MiB for render/compose/validate/
  score (SAML precedent), 8 MiB for capture (payload-bearing private triage
  mode); oversize fails closed with 413.
- Errors use `query.WriteErrorEnvelope`; successes use `query.WriteSuccess`
  except render, which returns raw bytes with the format content type.
- Telemetry: `query.RequestMetricsMiddleware` already records
  per-endpoint duration/error metrics for every `apiMux` route; auth via the
  existing `wrapAPIAuth` layer. No per-handler instrumentation to add.
- No MCP tools: these are CLI-support endpoints, not user-facing query
  capabilities, so the capability matrix and tool surface do not change
  (`mcp-schema-drift` unaffected — verified by the surface check).
- Differential proof: each endpoint's contract test asserts byte-identical
  output to the local function for the same input (the S4 parity proof);
  CLI thin-client wiring follows after S3 stubs land (separate change).

## NOT_CHECKED in this lane

- `local-host/v1` design for local-owner modes (named future route).
- `ScoreReport` endpoint (report-level gate; follows the same pattern when
  a consumer needs it).
- CLI thin-client wiring (post-S3; call sites listed in the follow-up).
