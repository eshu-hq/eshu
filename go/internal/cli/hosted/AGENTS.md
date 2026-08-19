# AGENTS.md — go/internal/cli/hosted guidance for LLM assistants

## Read first

1. `go/internal/cli/hosted/README.md` — purpose, ownership boundary, the
   redaction contract, and the network/environment surface.
2. `go/internal/cli/hosted/doc.go` — the godoc contract.
3. `go/cmd/eshu/hosted.go` — the cobra wrappers for both `hosted-setup` and
   `hosted-onboard`. They show which half owns what: flags, streams, the HTTP
   client, and the exit code live there.
4. `go/internal/cli/mcpsetup/AGENTS.md` — the token-redaction and snippet
   helpers this package calls.

## Invariants this package enforces

- **No process wiring here.** No cobra flags, no endpoint or credential read
  from the process environment, no network call, no `os.Exit`. `go/cmd/eshu` is
  `package main`, so nothing can import it — anything that reads a flag or maps
  to an exit code has to live in the wrapper instead.

  The one filesystem call is `os.WriteFile` inside `WriteArtifact`, against a
  path parameter the caller supplies. That is the same "act on an explicit
  parameter" shape as `internal/cli/mcpsetup`'s `WriteMCPServerConfig`. Do not
  "fix" it by pushing the write into the wrapper.
- **`Result.Connected` is true only when the bounded query returned.** Health,
  readiness, or a visible MCP tool surface is never on its own success. Any
  change that lets an earlier stage set `QueryAnswered` is a correctness bug.
- **A broad rule set is refused before any `Deps` seam is called.** The refusal
  in `ExecuteOnboard` runs before `ExecuteSetup`. If you add work ahead of it,
  `TestOnboardBroadRulesRejectedWithoutConfirm` fails the seams it stubs.
- **`FailCategory` values never collapse.** empty-index, partial-readiness,
  stale-readiness, missing-repo-scope, auth-unavailable, unreachable, and
  query-failed each send an operator somewhere different. Merging two of them
  into a generic failure is a product regression, not a simplification.
- **The artifact never carries a token value and never carries endpoint
  userinfo.** Every field that can hold an endpoint goes through
  `evidredact.Endpoint`, including the setup snippet, which is re-rendered here
  rather than copied from `Result.SetupHint`.

## Common changes and how to scope them

- **Add or change a connection stage** → edit `ExecuteSetup` in setup.go, add
  the `StageName` and any new `FailCategory` in report.go, and add its arm to
  `nextSteps`. A stage without a next step leaves the operator with a category
  and no action.
- **Change what counts as a broad rule** → edit `broadPatternSelectors` /
  `broadPatternReason` in rules.go, and add the shape to BOTH
  `TestClassifyRepoRulesBroadVariants` (the classifier) and
  `TestOnboardEveryBroadShapeIsRefused` (the refusal path). A classifier test
  alone does not prove the command refuses.
- **Add an artifact field** → add it to `Artifact` in onboard.go with its JSON
  tag, render it in `RenderArtifactMarkdown` and `RenderArtifactTerminal`, and
  decide its redaction before writing the code. If it can hold an endpoint or a
  credential, add a sentinel for it to `redaction_test.go`.
- **Change what the caller must supply** → edit `Deps` in deps.go and wire the
  new seam in `go/cmd/eshu/hosted.go`'s `hostedSetupDeps`.
  `TestHostedSetupDepsWiresEverySeam` fails on a seam left nil, because a nil
  seam panics at the stage that calls it.

## Failure modes and how to debug

- Symptom: hosted-setup reports `unreachable` where a 401 was expected → the
  transport error did not carry a readable status. The rule lives in
  `classifyProbeError`, which reads the code through `apierr.StatusCode`; the
  concrete error type must implement `apierr.HTTPStatusError`, the way
  `go/cmd/eshu/client.go`'s `apiHTTPError` does. Check that error type first.
- Symptom: `--repository` reports missing for a repository that is indexed →
  the `Repository.ScopeMatch` predicate, not this package. The wrapper builds it
  per entry in `hostedRepositoryList`; a closure shared across the loop makes
  every entry match the last repository.
- Symptom: index readiness disagrees with `eshu scan` → this package classifies
  a `scan.ReadinessVerdict` it is handed, it does not compute one. The status
  decode lives in the wrapper's `hostedReadinessVerdict`, and the evaluation is
  `scan.EvaluateReadiness` — the same function `eshu scan` runs, so a
  disagreement means the two commands read different deployments, not different
  rules.

## Anti-patterns specific to this package

- **Reaching into `go/cmd/eshu`.** It cannot be imported (`package main`). If
  new logic needs something only the wrapper has — a cobra flag, the HTTP
  client, the real process environment — add a parameter or a `Deps` seam.
- **Copying `Result.SetupHint` into the artifact.** It is rendered from the raw
  endpoint. That is correct for the operator-facing command and wrong for a
  shareable artifact; `buildArtifact` re-renders from the redacted endpoint on
  purpose.
- **Writing a redaction canary keyed on field names.** Plant the sentinel inside
  a VALUE — an endpoint password, a token — and assert absence from every
  rendering. A canary that only checks sensitive-looking keys is blind to the
  leak this package already shipped once.
- **Copying `evidredact.Endpoint` into this package.** This package carried a
  private userinfo-only copy before `internal/cli/evidredact` existed, and the
  copy fell behind the shared rule (no query-value or fragment scrubbing). One
  rule, imported, is the contract now. `normalizeArtifactFormat` is different:
  the evidence-report family keeps its own format vocabulary on purpose so the
  two artifacts have independent lifecycles.

## What NOT to change without an ADR

- Making `Result` redact its `ServiceURL`, `SetupHint`, or stage details.
  hosted-setup's output is read by the operator who owns the credential and
  deliberately shows the endpoint verbatim so a typo is visible. Changing it
  changes the operator's debugging surface.
- Moving readiness evaluation or repository-selector matching into this package.
  Both are shared with other CLI commands; a second implementation here would
  drift from the local `eshu scan` flow.
