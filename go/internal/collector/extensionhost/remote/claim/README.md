# Remote Claim Client (prototype seam)

## Purpose

This package is the prototype remote claimed-service client seam for the
extension-host extraction (`eshu-hq/eshu#8877`). It speaks the merged
`workflow.v1` protocol table from `eshu-hq/eshu-workflow`: claim via
`ClaimNext`, commit via `ValidateAndPinClaimForCommit` plus the
claim-lifecycle operations, and live producer-grant re-read via a
`GrantReader`. It exists so the section 7 hostile-input sites from
`go/cmd/collector-component-extension/EXTRACTION_PREP.md` can be replayed
against the exact protocol the extracted host will use, before any
production wiring moves.

## Ownership boundary

This package owns the HTTP claim/commit client and the grant-authority
check under a construction-time holder identity. It explicitly does not
own fact commit (core-owned ingestion stays core-side), grant issuance or
storage, claim dispatch, or the extension runner. Nothing in the
production path imports this package: in-tree `ClaimedService` remains
the only workflow claim mutation owner until the cutover harness wires
the extracted binary.

## Exported surface

- `Config`, `NewClient`, `Client` — seam construction and the claim,
  commit, and grant operations
- `ClaimNext`, `Heartbeat`, `Complete`, `Release`, `FailRetryable`,
  `FailTerminal`, `PinForCommit` — protocol operations
- `GrantReader`, `RegistryGrantReader`, `ReadLiveGrants`,
  `AuthorizeEmission`, `VerifyWiring`, `DeclaredEmission` — live-grant
  re-read and single-holder authority
- `Observer`, `Decision`, `DecisionStage`, `StageWiring`,
  `StageEmission` — grant-decision observation (never changes outcomes)
- `CallError`, `PacedError`, `DenyError` — typed failures: stale fence,
  pacing deferral, and grant denial
- `DefaultMaxResponseBytes`, `CodeResponseTooLarge` — response bound

See `doc.go` for the full godoc contract.

## Dependencies

- `github.com/eshu-hq/eshu-workflow/workflowv1` — merged protocol table
  (INTERIM-TILL-TAGGED path replace in `go/go.mod`; cutover pins a tag)
- `go/internal/component` — `ProducerGrant`, `EvaluateEmission`,
  `ClassifyEmission`, deny reasons (single authority implementation,
  reused not reimplemented)

## Telemetry

No metrics, spans, or logs are emitted: the seam is unwired prototype
code with no production caller.

- No-Observability-Change: no operator signal changes in this package.
- No-Regression Evidence: additive-only package; the in-tree
  `extensionhost` and `collector-component-extension` suites run
  unchanged (see the commit report).

Grant decisions reach operator telemetry at cutover through `Observer`,
which the extracted host will back with its grant telemetry recorder.

## Gotchas / invariants

- Holder identity is fixed at construction. `AuthorizeEmission` takes no
  producer argument by design: a tampered server response cannot reassign
  the holder.
- Every response body is strictly decoded (unknown fields and trailing
  data rejected) and every grant-bearing response is validated before
  use. The merged table declares no success body for lifecycle
  operations, so bytes on that path fail closed.
- The merged table declares no `CheckClaimPacing` response body and no
  empty-queue signal: pacing consults stay out of the seam (paced flow is
  covered via `ClaimNext` deferrals and 429s), and `ClaimNext` maps 404
  `not_found` to the empty-queue signal. Both are recorded cutover gaps.
- The bearer token travels as an `Authorization` header only and is
  scrubbed from every surfaced error, including hostile-server echoes.
- Errors carry identifiers and short reasons only, bounded to 512
  characters. Never add payloads, bodies, paths, or credentials.

## Related docs

- `go/cmd/collector-component-extension/EXTRACTION_PREP.md` §7
  (replayed hostile-input sites; mapping table in `replay_test.go`)
- `go/internal/collector/extensionhost/AGENTS.md` (host invariants)
- `docs/public/reference/local-testing.md` (gate source of truth)
