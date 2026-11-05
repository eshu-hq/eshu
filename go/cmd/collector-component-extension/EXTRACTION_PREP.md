# Extension Host — Extraction Prep Census (`#8877`)

Status: **PREP ONLY**. No code moves, no repo changes outside this file, no
deploy, no in-tree removal. This manifest records the current in-tree truth
for the extension-host surface so the cutover gate (`#4047`) can compare
in-tree and external outputs on identical inputs.

Scope: `go/cmd/collector-component-extension/` (worker binary),
`go/internal/collector/extensionhost/` (claim-aware host library), and
`go/internal/component/` (registry, trust policy, producer grants).
Out of scope: `sdk/go` and `eshu-sdk` modules (Codex `#8851`), canonical
projection (core-owned), all other collectors, org repos, deployment.
Peer scopes reused, not redone: numeric fidelity (`#8936` merged),
CLI/grant/conformance (`#8909` open), Crossplane replay (`#8948` open).

Part of `#6707`. Keep this a direct execution record; no intermediate epic.

## 1. Current packages

### Binary: `go/cmd/collector-component-extension/`

The binary does **not** install components; installation is owned by
`component.Registry.Install` (`go/internal/component/registry.go::Install`). The
binary selects one trusted claim-capable activation and serves claims.

| File | Role | Key symbol |
| --- | --- | --- |
| `main.go:46,108` | entry point, telemetry bootstrap, Postgres open, status server, `signal.NotifyContext` cancellation | `run` |
| `service.go:28` | claimed-service wiring | `buildClaimedService` |
| `service.go:71,76` | per-emission live-grant re-read (revoked-during-execution fails next result closed; unreadable set denies core-owned kinds as `grants_unreadable`) | `LiveGrants` |
| `service.go:44,65` | host-minted App token per claim; private key never crosses the boundary | `tokenProviderFromEnv` |
| `config.go:163` | exactly-one trusted claim-capable activation or fail closed (zero or ambiguous matches are startup errors) | `selectActivation` |
| `config.go:381` | trust policy from env (mode default disabled; allow/revoke ID and publisher lists; core version) | `componentPolicyFromEnv` |
| `config.go:248` | activation-config read errors name only the variable, never its value | `sanitizedActivationConfigReadError` |
| `config.go:359` | GitHub App token provider from env; malformed mapping fails startup closed | `tokenProviderFromEnv` |

### Library: `go/internal/collector/extensionhost/` (moves with the host)

| File | Role | Key symbol |
| --- | --- | --- |
| `doc.go:4` | boundary contract: bounded JSON SDK over stdin/stdout; claim mutation, stale-fence handling, and commits stay outside the extension boundary | package doc |
| `source.go:27` | activation gate: manifest grant observation, then `ValidateWithGrants` fail-closed without grants | `NewSource` |
| `source.go:110,135` | per-claim run under `context.WithDeadline(ctx, request.Claim.Deadline)` | `nextClaimed` |
| `source.go:216` | result validation chain: SDK validator, bounded statuses, claim-identity match, payload schemas, grant coverage | `validateResult` |
| `source.go:266` | per-emission live-grant recheck; core-owned kind without a live grant fails closed | `validateGrantCoverage` |
| `process.go:23,43` | local-process runner over JSON stdin/stdout; `exec.CommandContext` cancellation; 4 MiB stdout / 64 KiB stderr caps, truncation fails closed | `ProcessRunner` |
| `oci.go:42,119` | digest-pinned OCI runner (`@sha256:<64 hex>` required, floating tags rejected); `--network none`, `--read-only`, non-root user, `--cap-drop ALL`, `no-new-privileges`; host env never forwarded; no datastore/graph handles | `OCIRunner` |
| `oci.go:168` | shared strict result decoder: `UseNumber`, `DisallowUnknownFields`, trailing-JSON rejection (`#8936` numeric fidelity) | `decodeSDKResult` |
| `mapping.go:53` | validated SDK facts mapped to envelopes, deduplicated by kind + stable key | `envelopesForResult` |
| `failure.go`, `claim_outcome.go` | failure taxonomy: `invalid_claim`, `invalid_result`, `identity_mismatch`, `launch_failure`, `status_record`; `claim_outcome.go` adds `source_state_record` (#9420) | failure classes |
| `grant_decision.go` (in `component/`) | deny reasons `revoked`, `expired`, `grants_unreadable` | `GrantReason*` |

### Library: `go/internal/component/` (trust + authority; moves with the host)

| File | Role | Key symbol |
| --- | --- | --- |
| `registry.go:65` | component installation (binary explicitly does not install) | `Install` |
| `registry.go:150` | live producer-grant read | `ProducerGrants` |
| `policy.go:51,58` | trust verification, with and without grants | `Verify`, `VerifyWithGrants` |
| `producer_grant.go:41,43` | explicit revocable authority: `ExpiresAt` (zero means expired), `Revoked` (never matches) | `ProducerGrant` |
| `producer_grant.go:93` | emission authorization | `EvaluateEmission` |
| `grant_decision.go:43,45,56` | deny reasons | `GrantReasonRevoked`, `GrantReasonExpired`, `GrantReasonGrantsUnreadable` |

## 2. Fact kinds and admission

Extension-emitted kinds are manifest-declared, not host-enumerated: the
manifest's collector kinds gate claim acceptance (`source.go:201`), and every
emitted core-owned kind needs a live producer grant (`source.go:266`).
Core reservation is `facts.IsCoreFactKind`
(`go/internal/facts/registry.go::IsCoreFactKind`). The generated registry
(`go/internal/facts/fact_kind_registry.generated.go`) holds 185 `Kind:`
entries (counted live 2026-10-28). Extension facts are source evidence until
a reducer owns a consumer contract — no graph writes from this surface
(`go/cmd/collector-component-extension/AGENTS.md` invariants).

## 3. Consumers (must agree at cutover)

- Claim control: `collector.ClaimedService` (heartbeat below lease TTL).
- Fact commit: core-owned Postgres ingestion store (`service.go:83`).
- Claim lifecycle store: core-owned workflow control store (`service.go:87`).
- Status/telemetry: `StatusTelemetry` production recorder; grant decisions
  observed via `GrantTelemetry` (`service.go:38`).
- Reducer, canonical graph, HTTP API, and MCP agreement on the extracted
  path: `NOT_CHECKED`.

## 4. Durable-state owner

The host owns **no** durable state. Durable writes stay core-owned:
fact commit, claim control, and claim lifecycle (`service.go:83,87`).
In-tree transient state only: per-claim request/config clones and runner
handles. Generation IDs and fencing tokens arrive on the work item and are
reused on retry, so redelivery stays idempotent. Admission, persistence,
reducer queue, projection, and retraction remain Eshu core. The extracted
producer must submit a bounded result; it never receives Postgres, graph,
queue, lease, reducer, API, MCP, or workflow handles.

## 5. Protocols

- Bounded JSON SDK contract over stdin/stdout (`doc.go:19`); at cutover the
  OCI runner speaks the same contract as the process runner from a
  digest-pinned artifact.
- Claim/lease/heartbeat/commit through `collector.ClaimedService`; the
  extension never touches claim mutation.
- Public target contract pins: `eshu-sdk/collector v0.2.0-rc.3`,
  `eshu-sdk/factschema v0.1.0-rc.17` (`go/go.mod:159-160`, moved to rc.17
  by #10043 on 2026-11-05); toolchain directive `go 1.26.10`, installed `go1.26.6`
  (mismatch recorded, no behavior impact on this doc).
- Claim-plane pin: `eshu-workflow v0.1.0` (`go/go.mod:161`); protocol
  major `1` only (pairs recorded below).
- Supported oldest/newest pairs for this boundary (box-9 record):
  - SDK: `collector v0.2.0-rc.3` + `factschema v0.1.0-rc.17` (live pins
    above). The supported-combination policy lives in
    `docs/public/extend/sdk-compatibility.md` (initial `v0.1.0` pair,
    `v0.2.0` pair, historical `collector v0.1.0-rc.3` +
    `factschema v0.1.0-rc.5` proxy-verified pair).
  - Claim plane: `workflow.v1` oldest = newest = `v1` (single-major
    protocol), PROVEN-in-harness by the §1 pin test plus
    `TestClaimPlaneClient*` 8/8 PASS
    (`go/cmd/workflow-coordinator/PLANE_DECLARATION_9376.md` §3,
    accepted as contract 2026-11-02).
  - Host × core matrix: newest-host × oldest-core and reverse, run in
    `eshu-hq/eshu-extension-host` once scaffolded (prereq §10.3). The
    destination is still a stub, so the matrix run is pending — box 9
    stays Partial until it lands.

## 6. Artifacts

- Today: single in-tree binary `collector-component-extension`
  (`main.go:24` version flag). No published artifact. No cutover has
  occurred.
- Destination `eshu-hq/eshu-extension-host` is a pre-release stub
  (root `{README.md, Makefile, CONTRIBUTING.md, .github/}` only, HEAD
  `6a5d1848`, observed 2026-11-04 via API; holds no cutover authority).
  The `#8877` body cites `50e55647…`, which is eshu-main merge commit
  `50e556470f` (the inventory commit at issue-writing time, per the
  2026-10-28 issue comment) — never a destination SHA, so no destination
  reconcile is owed for it. Scaffolding the host implementation is cutover
  work staged behind `#9376` → `#9381`.
- No artifact is built or published by this PREP.

## 7. Hostile-input proof sites (in-tree side, all passing at HEAD)

| Threat | Proof |
| --- | --- |
| Revoked grant at wiring | `grant_signal_test.go:143` `TestBuildClaimedServiceRevokedGrantFailsClosedAndRecordsDeny` — revoked grant stops the worker AND records `deny/readback/revoked` |
| Revoked/expired at emission | `producer_grant_decision_test.go:63` (revoked/expired rows expect `revoked`/`expired`), `go/internal/collector/extensionhost/producer_grant_test.go::TestSourceRejectsEmissionAfterGrantRevoked`, `go/internal/collector/extensionhost/producer_grant_test.go::TestSourceRejectsEmissionAfterGrantExpiry` (post-revoke, post-expiry rejection; the `go/internal/component/` copy of that filename is a different `RecordGrant`/`Install` test) |
| Sandbox escape / floating image | `oci_test.go:29,57,113` — digest pin + isolation argv, unpinned rejection before exec |
| Resource exhaustion | `oci_test.go:148` stdout-limit enforcement; `process.go:19` 4 MiB/64 KiB defaults; truncation fails closed |
| Cancellation | `oci_test.go:165` canceled-context honor; `process.go:53` and `oci.go:89` return `ctx.Err()`; per-claim deadline `source.go:135` |
| Malformed results | strict decoder (`oci.go:168`); trailing-JSON rejection; `validation_test.go:14,31` claim/status validation |
| Identity mismatch | `validateReturnedClaim` in `source.go:216` chain (`identity_mismatch` terminal) |
| Credential leakage | `config.go:248` sanitized read errors; static value under the host-minted key rejected at activation (`source.go:50`); only the minted token crosses stdio (`token_provider.go`, `doc.go:38`) |
| Grant-before-admission | activation gate (`source.go:27`) plus per-emission recheck (`source.go:266`); unreadable set denies core-owned kinds |

## 8. Migration stage

`contract_blocked` (amended 2026-10-28; see §11).
Production acceptance, deployment, and rollback: `NOT_CHECKED`.

Rollback-rehearsal draft for this boundary (DRAFT — not executed;
rehearsal stays post-authority per the `#9383` disposition, which rules
delete-path-suffices conditional on zero satellite-owned durable state
and voids if `#9381` wiring adds any satellite store):
1. Record pre-state: deployed digests, ArgoCD sync revision, active /
   superseded / failed generation counts, pending-claims proof.
2. Fence claims and revoke replaced authority; prove no dual authority.
3. Delete the satellite path back to the first claim; measure RTO.
4. Restore prior compatible artifacts/configuration; verify truth,
   availability, and durable-state compatibility.
5. Replay safely: exactly-once resume, revoked-grant replay, zero-state
   audit. The rehearsal must pass on the exact deployed digest.

## 9. `#8877` acceptance sweep at `origin/main` (`dc65b4d597`)

| Acceptance item | State |
| --- | --- |
| Inventory manifest/trust/install/execution, result limits, policy, protocols | **Met in PREP** by §§1–6 above |
| Revoked/expired/sandbox/cancellation/malformed/redaction/grant-before-admission | **Met in-tree side** by §7 proof sites (all in the focused suite below) |
| Named owners, production target, configuration source | **Partial** — release/deployment owner confirmed on the issue; destination stub and config source recorded in §6 with the SHA discrepancy flagged |
| Packages, kinds, consumers, state, protocols, artifacts, stage, prerequisites | **Met in PREP** by §§1–6, 8–9 |
| Behavior and public-contract parity, representative + hostile | **Open** — in-tree proof sites recorded; extracted-path parity needs the cutover harness |
| Extracted-path reducer/graph/API/MCP truth | **Open** (`NOT_CHECKED`) |
| Concurrent retry/duplicates/ordering/cancellation/revocation/staleness/partial failures | **Open** — single-claim proofs exist; contention/ordering/duplication on the extracted path need live proof |
| Bounded resources, no regression, health/readiness/telemetry | **Open** on the extracted path; in-tree suite green (see evidence) |
| Credentials, provenance, vuln response, oldest/newest pairs | **Partial** — handling recorded here; pairs recorded in §5 (SDK pins + plane v1/v1 + matrix policy, re-verified 2026-11-04); host-core matrix run pending in the extension-host repo |
| Independent build/release/deploy on public contracts | **Open** — destination is a stub; no Deployment/Service in `iac-eks-eshu` for this host (verified: zero matches for `extension-host` under `argocd/eshu` and `chart/`) |
| Rollback rehearsal | **Open** — draft checklist recorded in §8 (not executed; rehearsal stays post-authority per the `#9383` disposition) |
| Review, local proof, stable CI, removal after acceptance | **Open** — local proof in this lane's commit; removal explicitly **not** in PREP |

## 10. Prerequisites for cutover

1. Published SDK + fact-schema modules (`#6708`, `#8851`) at the §5 pins.
2. First-party producer delegation live (`#6709`); destination SHA
   reconciled (§6).
3. Host repository from the `#6710` template with the compatibility
   harness matrix (newest host × oldest core and reverse).
4. Parity proof on representative + hostile inputs against this PREP's §7
   sites, run on the extracted path.
5. Producer-authority single-holder enforcement and a rehearsed rollback
   draft for this state boundary.
6. `#4047` per-collector cutover gate verdict on identical inputs.

## 11. Stage amendment — arbiter-eshu revised ruling (2026-10-28)

- Stage is now `contract_blocked` on `#9381`, sequenced after `#9376`:
  the host must not grow an interim protocol — claim/lease/commit moves
  onto the workflow.v1 claim plane once that lands, with no stopgap wire
  format in between.
- SHA note: `50e55647` is the eshu-main inventory merge commit at
  issue-writing time, not the destination SHA; the destination stub HEAD
  recorded in §6 is reconciled at cutover.
- Stage comments already posted on `#8877`; not reposted.
