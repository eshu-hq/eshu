# AGENTS.md — go/internal/cli/investigation guidance for LLM assistants

## Read first

1. `go/internal/cli/investigation/README.md` — purpose, ownership boundary,
   exported surface
2. `go/internal/cli/investigation/doc.go` — the godoc contract
3. `go/cmd/eshu/investigation_cmd.go` — the cobra `RunE` wrapper. It is the
   only place flags, the API client, the output streams, and the exit code are
   resolved, and it shows how the two halves fit together.
4. `go/internal/query/investigation_packet.go` — the packet schema and the
   refusal states this package selects between. Do not restate the schema here.

## Invariants this package enforces

- **No process wiring.** No cobra import, no environment reads, no
  `os.Stdout`/`os.Stdin`, no exit-code mapping. `go/cmd/eshu` is `package main`
  and cannot be imported, so anything reading a flag or choosing an exit code
  has to live in `investigation_cmd.go` instead. `WriteArtifact` takes two
  `io.Writer` parameters for exactly this reason; the wrapper passes
  `cmd.OutOrStdout()` and `cmd.ErrOrStderr()`.

  `WriteArtifact` calling `os.WriteFile` on its `out` parameter is *not*
  process wiring — it acts on an explicit argument, the same shape as
  `internal/cli/servicereport`'s `ReadInput`. Do not "fix" it by moving the
  write into the wrapper.

- **A transport error is classified by status alone.** `RefusalFromFetchError`
  reads `apierr.StatusCode(err)` and nothing else. `eshu trace` puts two
  `strings.Contains` checks *ahead* of its status switch; this family
  deliberately has neither. Adding one silently changes which errors become
  refusal packets.

- **Fetches return the transport error unwrapped.** Wrapping breaks nothing for
  `errors.As`, but it does change the text an operator reads on a failed call.
  The `//nolint:wrapcheck` on each `Fetch*` return is deliberate: without it the
  linter pushes you toward a wrap that rewrites CLI output.

  These three findings *would* disappear if the code moved back under `cmd/`,
  so do not defend them on the grounds that wrapcheck would follow the code. It
  would not follow, for two separate reasons.

  First, wrapcheck reads the package differently depending on what is being
  called, and the rule people usually quote is not the one that applies here:

  - A call on a **concrete** type is matched against the package the error
    **comes from**. `wrapcheck.ignore-package-globs` lists
    `github.com/eshu-hq/eshu/go/cmd/*`, so an error originating inside a
    `go/cmd/*` package is ignored wherever it is returned, and an `os.ReadFile`
    error is still reported wherever the calling code sits.
  - A call on an **interface** method is matched against the package that
    **declares the method**. `Client` is declared in this package next to the
    `Fetch*` functions, so origin and location are the same package here, and
    the `go/cmd/*` glob covers these calls the moment the file moves under
    `cmd/`.

  Second, `go/.golangci.yml` also carries a plain `path: 'cmd/'` exclusion that
  switches wrapcheck off for every file under `cmd/` no matter where the error
  came from. Either mechanism alone is enough.

  The real reason this code stays here is that `go/cmd/eshu` is `package main`
  and cannot export `Client` to anybody — which is the whole point of the
  extraction. Three `//nolint` directives are what that costs.

- **Server text never enters the artifact.** An envelope `error.message` and a
  transport error string go to stderr through the CLI error only. The refusal
  packet carries family, scope, and refusal state — nothing else.
  `TestArtifactDropsServerAndTransportText` checks 162 renderings for this.

## Common changes and how to scope them

- **Add a scope key an existing family understands** → edit that family's
  derivation (`SupplyChainFilterFromSubject`, `DeployableUnitParams`, or
  `DriftRequestBody`) plus its scope predicate. `ParseSubjectFlags` is
  key-agnostic and needs no change.
- **Add a family** → add a `build<Family>Packet` in packet.go, a `Fetch*` and a
  `Deps` field in fetch.go, wire it in `DefaultDeps`, and add the `case` to
  `BuildPacket`. Leaving the `Deps` field unset panics rather than failing;
  `TestDefaultDepsWiresEveryFamily` is the guard.
- **Map a new HTTP status or envelope code to a refusal** → edit
  `RefusalFromFetchError` or `RefusalFromErrorCode` in refusal.go and extend the
  table in `refusal_test.go`. Both classifiers return `(state, false)` for
  anything unmapped so an unknown failure surfaces to the operator rather than
  becoming an artifact that looks like an answer.
- **Change the artifact's file permissions or the stderr confirmation** →
  `WriteArtifact` in artifact.go only.

## Failure modes and how to debug

- Symptom: a scope that should work produces a `scope_not_found` packet →
  check the family's scope predicate before suspecting the API.
  `SupplyChainFilterHasScope` requires a finding id, *or* an advisory/CVE paired
  with a package, repository, or subject digest; an advisory alone refuses on
  purpose. `DeployableUnitParams` requires both `scope_id` and `generation_id`.
- Symptom: the command exits 1 where an artifact was expected → the failure was
  not classifiable. An unmapped envelope code produces `read failed: <code>:
  <message>`; an unclassifiable transport error is passed through verbatim.
- Symptom: a status-carrying error stops classifying after a refactor → someone
  wrapped a `Fetch*` return with something that does not implement `Unwrap`, or
  `go/cmd/eshu`'s `apiHTTPError` lost its `HTTPStatusCode` method. `client.go`
  carries a compile-time assertion against `apierr.HTTPStatusError` for the
  second case.

## Anti-patterns specific to this package

- **Reaching into `go/cmd/eshu`.** It cannot be imported. If logic here needs
  something only the wrapper has, add a parameter or a `Deps` field.
- **Turning an error into an empty packet.** The `default` branch of
  `BuildPacket` errors on a recognized-but-unwired family precisely so an
  operator never reads "not implemented" as "nothing found".
- **Putting a `--subject` value somewhere new without checking where it lands.**
  Scope values are already copied into `identity.subject` and into the request
  query string. Any new carrier needs a case in `redaction_test.go`.

## What NOT to change without an ADR

- Moving packet composition (`query.BuildSupplyChainImpactPacket` and its
  siblings) into this package. The split keeps the schema and its validation in
  `internal/query`, where the API and MCP surfaces share it.
- Making `RefusalFromFetchError` inspect the error message. That would align
  this family with `eshu trace` and change which failures become artifacts; it
  is a contract decision, not a refactor.
