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
- `CutoverConfig`, `Cutover`, `NewCutover`, `ErrCutoverDisabled` —
  default-off cutover gate (Box 1, `#9381`): flag-on routes claim,
  commit, and live-grant re-read through the Client; flag-off keeps the
  in-tree path with no wire, grant-read, or observer side effects

See `doc.go` for the full godoc contract.

## Dependencies

- `github.com/eshu-hq/eshu-workflow/workflowv1` — merged protocol table
  (pinned at `v0.1.0` in `go/go.mod`); tests also import
  `github.com/eshu-hq/eshu-workflow/server` for the contract proof, with a
  stubbed storage interface (no database)
- `go/internal/component` — `ProducerGrant`, `EvaluateEmission`,
  `ClassifyEmission`, deny reasons (single authority implementation,
  reused not reimplemented)

## Telemetry

No metrics, spans, or logs are emitted: the seam is unwired prototype
code with no production caller.

- No-Observability-Change: no operator signal changes in this package.
- No-Regression Evidence: additive-only package with no production
  importer. The in-tree `extensionhost` and `collector-component-extension`
  suites run unchanged and green:
  `cd go && go test -race -count=1 ./internal/collector/extensionhost/... ./cmd/collector-component-extension/...`.

Grant decisions reach operator telemetry at cutover through `Observer`,
which the extracted host will back with its grant telemetry recorder.

The Box 1 cutover gate (`wiring.go`, `#9381`) is default-off with no
production importer: flag-off reports `ErrCutoverDisabled` with no wire,
grant-read, or observer side effects, and flag-on delegates to the `Client`
proven above.

- No-Regression Evidence: baseline `origin/main@446af11c8e` package suite
  green; after `go test -race -count=1
  ./internal/collector/extensionhost/remote/claim/` green including 9 new
  `TestCutover_*` (flag-off sentinel parity, flag-on claim/commit routing
  via the pinned fake, wired-vs-in-tree authority match on 7 grant sets,
  second-producer denial, 8x25 concurrent-use race probe). No queues,
  workers, batches, or locks added or retuned: the gate holds one
  read-only bool plus the bound `Client`, and the `WaitGroup`/`go func`
  content hits live in the race-probe test only.
- No-Observability-Change: the gate emits no metrics, spans, or logs; the
  bound `Client` observes grant decisions only when enabled.

## Gotchas / invariants

- Holder identity is fixed at construction. `AuthorizeEmission` takes no
  producer argument by design: a tampered server response cannot reassign
  the holder.
- Every response body is strictly decoded (unknown fields and trailing
  data rejected) and every grant-bearing response is validated before
  use. A lifecycle operation (heartbeat, complete, release, fail) succeeds
  only on the exact `{"ok":true}` acknowledgement the pinned server writes;
  an empty body, `ok:false`, an unknown field, or trailing data fails
  closed. `contract_test.go` runs the Client against the pinned
  `eshu-workflow/server` handler so these shapes come from the satellite,
  not from the test fake.
- An issued grant must match the requested selector (claim id, owner id,
  collector kind, collector instance id); a mismatch fails closed.
- A 429 `paced` answer without `retry_after_seconds` yields a zero
  `PacedError.RetryAfter` (the field is optional in the table). The caller
  applies its own backoff floor.
- The default clock reads the wall clock on every call, so an expired grant
  denies even when `Config.Clock` is nil.
- The merged table declares no `CheckClaimPacing` response body and no
  empty-queue signal: pacing consults stay out of the seam (paced flow is
  covered via `ClaimNext` deferrals and 429s), and `ClaimNext` maps 404
  `not_found` to the empty-queue signal. Both are recorded cutover gaps.
- The bearer token travels as an `Authorization` header only and is
  scrubbed from every surfaced error, including hostile-server echoes.
- Errors carry identifiers and short reasons only, bounded to 512 bytes on
  a rune boundary after the bearer is scrubbed (scrub first, then bound).
  This covers the pin-refusal text and the pacing domain in `PacedError`.
  Never add payloads, bodies, paths, or credentials.

## Related docs

- `go/cmd/collector-component-extension/EXTRACTION_PREP.md` §7
  (replayed hostile-input sites; mapping table in `replay_test.go`)
- `go/internal/collector/extensionhost/AGENTS.md` (host invariants)
- `docs/public/reference/local-testing.md` (gate source of truth)
