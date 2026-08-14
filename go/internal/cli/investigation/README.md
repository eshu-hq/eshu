# Investigation Evidence Packets

## Purpose

This package builds the artifact `eshu investigation export` writes. Given a
family, a scope, and an optional bounds override, it reads one Eshu API route,
maps the response into an `investigation_evidence_packet.v2`, and hands the
packet back for rendering.

Three families are wired: `supply_chain_impact` reads
`/api/v0/supply-chain/impact/explain`, `deployable_unit` reads
`/api/v0/evidence/admission-decisions`, and `drift` posts to
`/api/v0/cloud/runtime-drift/findings`.

The logic lives here rather than in `go/cmd/eshu` because that directory is
`package main` and nothing can import it. Refusal classification, subject
parsing, and filter derivation were untestable from outside the binary while
they lived there.

## Ownership boundary

This package owns the mapping from an operator's scope to an API request, and
from an API response to a packet. It does not own the packet schema, the
renderers, or the validation gate — those are `internal/query`
(`NewInvestigationEvidencePacket`, `BuildSupplyChainImpactPacket`,
`BuildDeployableUnitPacket`, `BuildDriftPacket`, `RenderInvestigationPacket`).

It also does not own anything that touches the process. Cobra flags, the API
client's construction, `cmd.OutOrStdout()`, the environment, and the exit code
all stay in `go/cmd/eshu/investigation_cmd.go`.

## Exported surface

Packet building:

- `Request` — family, subject scope, optional `*query.PacketBounds`.
- `BuildPacket(Client, Deps, Request)` — dispatches by family.

Transport:

- `Client` — the two-method port (`GetEnvelope`, `PostEnvelope`) that
  `go/cmd/eshu`'s `*APIClient` satisfies.
- `Deps` / `DefaultDeps()` — the per-family fetch seam.
- `FetchSupplyChainExplain`, `FetchAdmissionDecisions`, `FetchDriftFindings`.
- `SupplyChainExplainEnvelope`, `AdmissionDecisionsEnvelope`,
  `DriftFindingsEnvelope`.

Refusal classification:

- `RefusalFromFetchError` — HTTP status to refusal state.
- `RefusalFromErrorCode` — in-envelope `query.ErrorCode` to refusal state.
- `RefusalFromEnvelopeError` — the envelope wrapper around the above.

Scope handling:

- `ParseSubjectFlags`, `SubjectOrPlaceholder`, `ParseFamily`,
  `BoundsFromMaxSourceFacts`.
- `SupplyChainFilterFromSubject`, `SupplyChainFilterHasScope`,
  `DeployableUnitParams`, `DriftRequestBody`.

Output:

- `WriteArtifact(stdout, stderr io.Writer, out string, data []byte)`.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/query` — the packet schema, the per-family builders, the renderers,
  and the `ErrorCode` vocabulary.
- `internal/cli/apierr` — `StatusCode(err)`, the only way to read an HTTP status
  off a `go/cmd/eshu` transport error from an importable package.
- Standard library otherwise. No cobra: `go list -deps` on this package returns
  no `spf13` entry, and that is the machine-checkable form of the no-process-
  wiring rule.

## Telemetry

None. The package issues no spans, metrics, or logs — it is a mapping layer over
one HTTP call the caller's client makes. Operator-visible output is the artifact
itself plus the one stderr line `WriteArtifact` prints when `--out` is set.

## Gotchas / invariants

- **Status only, never message.** `RefusalFromFetchError` reads
  `apierr.StatusCode` and nothing else. `eshu trace` does the opposite: it checks
  the error text for `connection refused` and `request failed` *before* its
  status switch, so a 400 carrying that text classifies as `backend_unavailable`
  there and stays a plain CLI error here. If you ever add a message check,
  decide its precedence deliberately —
  `TestRefusalFromFetchErrorClassifiesByStatusNotMessage` pins today's answer.
- **Fetches do not wrap.** Each `Fetch*` returns the client's error unchanged so
  `errors.As` still finds the status and the operator reads the client's own
  text. The `//nolint:wrapcheck` directives on those returns are load-bearing,
  and moving the code somewhere else would not remove the need for them.
  `wrapcheck.ignore-package-globs` in `go/.golangci.yml` matches the package an
  error came *from*, not the package the code sits in. An unwrapped error
  originating in a `go/cmd/*` package is ignored wherever it is returned; code
  that merely lives under `cmd/` gets nothing. `go/cmd/eshu` is `package main`
  and nothing can import it, so it never supplies an error to another package
  either, which leaves that glob doing nothing at all here.
- **A refusal is not an error.** Every scope, transport, and envelope refusal
  path returns `(packet, nil)`. The command exits 0 and writes a valid artifact.
  Only an unmapped envelope code, an unclassifiable transport error, and a
  recognized-but-unwired family produce an error.
- **The subject reaches the artifact.** `SubjectOrPlaceholder` copies the
  operator's scope verbatim into `identity.subject`, which every renderer
  prints, and `FetchSupplyChainExplain` puts it in the request query string,
  which `net/http` quotes back inside a transport error. A password in
  `--service-url` does not survive that far — `net/http` replaces the userinfo
  password with `***` — but a secret in a `--subject` value does.
  `TestArtifactKeepsTheOperatorScope` and `TestSubjectValuesReachTheRequestURL`
  pin both halves.
- **`Deps` fields are not optional.** `BuildPacket` calls the field for the
  family it is building with no nil check, so a family left unset in a custom
  `Deps` panics. `DefaultDeps` sets all three and
  `TestDefaultDepsWiresEveryFamily` guards it.
- **Empty and whitespace-only filter fields are dropped** before the query
  string is built, so the API sees only what the operator named.

## Related docs

- `go/cmd/eshu/investigation_cmd.go` — the cobra wrapper, and the only place
  flags, streams, and the API client are resolved
- `go/internal/cli/apierr/README.md` — why the status crosses the package
  boundary through an interface
- `go/internal/query/investigation_packet.go` — the v2 packet contract
