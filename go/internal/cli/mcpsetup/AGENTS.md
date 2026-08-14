# AGENTS.md — go/internal/cli/mcpsetup guidance for LLM assistants

## Read first

1. `go/internal/cli/mcpsetup/README.md` — purpose, ownership boundary,
   exported surface
2. `go/internal/cli/mcpsetup/doc.go` — the godoc contract
3. `go/cmd/eshu/mcp_setup_cmd.go` — the cobra `RunE` wrapper that resolves
   process state (flags, `*APIClient`, output streams) and calls into this
   package. This is the file that shows how the two halves fit together.

## Invariants this package enforces

- **No process wiring in this package.** No cobra flags, no env reads that
  resolve Eshu config or a credential, no `fmt.Print*`. `go/cmd/eshu` is
  `package main`, so nothing can import it — any symbol that reads a flag,
  resolves `*APIClient` via `go/internal/cli/config.ResolveValue`, or maps to an exit code has
  to live in `mcp_setup_cmd.go` instead. If you find yourself wanting to call
  `cmd.Flags()` or read a credential out of the environment, that logic
  belongs in the wrapper, not here.

  Two env touches already live here and are deliberate, so do not "fix" them:
  `os.UserHomeDir` in `DescribeWriteTarget` (`write.go`), which only shortens
  a path for display, and the RFC 9728 discovery probe's own dedicated HTTP
  client. Neither resolves configuration or a credential, which is the line
  this invariant actually draws.
- **Never emit a raw secret.** `RedactToken` and `TokenReference` are the
  only sanctioned paths for putting a credential-adjacent string into
  rendered output, and neither ever returns the raw value. A new snippet
  renderer or guidance block MUST route through them, not `fmt.Sprintf` the
  token directly.
- **`AuthPosture`'s zero value is `PostureToken`.** Do not reorder the
  `iota` block in `posture.go`; tests and callers rely on the zero value
  being the safe per-user-token default.
- **`HealthProber`/`QueryProber` are the only seam into `*APIClient`.** This
  package must not import `go/cmd/eshu` (it cannot -- `package main`) or
  gain any other dependency on the CLI's client type. `apiHealthProber`/
  `apiQueryProber` in `mcp_setup_cmd.go` are the sole implementations.

## Common changes and how to scope them

- **Add a new supported MCP client (platform)** → add an entry to the
  `platforms` slice in `mcpPlatformRegistry()` (setup.go) with its own
  snippet renderer function in snippet.go. Why: the registry is the single
  source of truth for `--platform` values, target file hints, and
  env-var-token support; a renderer defined elsewhere silently never gets
  registered.
- **Change what `--verify` checks** → add a stage to `RunVerification` in
  verify.go (a new `VerifyStage` constant plus the stage function). Why:
  stages are ordered and reported uniformly by `RenderVerifyReport`; a check
  bolted on elsewhere breaks the `[ok]`/`[--]`/`[!!]` report shape.
- **Change credential resolution for `--auth`** → edit `ResolveAuthPosture`
  in posture.go, not the wrapper. The wrapper only supplies the probe
  function (`HostedPostureProbe`) and the flag values; the decision logic
  (explicit flag wins, `--shared-key` wins over `--auth auto`, auto probes
  only when hosted) lives here so it is unit-testable without cobra.
- **Change the `--write` merge target for a platform** → edit
  `DefaultWriteTarget` in write.go. Why: it is the single switch keyed on
  `Platform.Name` that the wrapper's `mcpSetupWrite` calls; a second switch
  elsewhere would drift.

## Failure modes and how to debug

- Symptom: a new platform's snippet renders but `--write` fails with "no
  default --write target" → cause: the platform was added to the registry
  in setup.go but `DefaultWriteTarget` in write.go was not updated to match.
  Both must be kept in lockstep for a platform marked `Writable: true`.
- Symptom: `--verify`'s first-query stage runs against the wrong credential
  → this is almost always a wrapper bug (`mcp_setup_cmd.go`'s
  `mcpSetupVerify`), not a bug in this package. This package only executes
  whatever `HealthProber`/`QueryProber` it is handed; it does not decide
  which credential backs them.
- Symptom: `TestDocLockstepLiterals` (doc_lockstep_test.go) fails after a
  constant value change → the sibling half of that lockstep guard is
  `scripts/verify-mcp-client-auth-doc.sh`, which greps
  `docs/public/operate/mcp-client-auth.md` for the same four literals. Update
  the doc, not just the test.

## Anti-patterns specific to this package

- **Printing from this package.** Every render function returns a string;
  every check function returns a result struct. `fmt.Print*` belongs only in
  `mcp_setup_cmd.go`.
- **Reaching into `go/cmd/eshu`.** It cannot be imported (`package main`).
  If new logic needs something only the wrapper has (a cobra flag, the
  resolved `*APIClient`), add a parameter or a narrow interface instead.
- **Hand-building a credential string instead of calling `TokenReference`.**
  Bypassing it is how a raw secret ends up in a snippet.

## What NOT to change without an ADR

- The `HealthProber`/`QueryProber` interface shapes — `mcp_setup_cmd.go`'s
  `apiHealthProber`/`apiQueryProber` implement them structurally; changing a
  method signature here breaks that implementation silently until the next
  build.
- The RFC 9728 discovery probe's dedicated 3-second timeout
  (`newPostureProbeClient` in posture.go) — an offline `--hosted` run must
  fail fast, not hang for the `*APIClient`'s longer default.
