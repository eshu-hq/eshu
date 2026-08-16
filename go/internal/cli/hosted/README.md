# Hosted Setup And Onboarding

## Purpose

Decision logic for the two hosted commands, `eshu hosted-setup` and `eshu
hosted-onboard`. It owns three things: the ordered connection checks against a
deployed Eshu service, the rule classification that refuses accidental org-wide
ingestion, and the redacted onboarding artifact a project team is handed.

Before this package existed the same code lived in `go/cmd/eshu`, which is
`package main` — nothing could import it, so none of it could be tested outside
the binary. The split (issue #6059) moved the decisions here and left the cobra
registration, flag reads, stream resolution, HTTP transport, and exit-code
mapping in `go/cmd/eshu/hosted_setup_cmd.go` and
`go/cmd/eshu/hosted_onboard_cmd.go`.

## Ownership boundary

This package owns:

- the six-stage order and each stage's `FailCategory`
- the narrow-versus-broad rule verdict and the refusal
- the artifact's shape, its redaction, and its Markdown/JSON/terminal renderings

This package does NOT own:

- the HTTP client, flag parsing, stream resolution, or exit codes — all in
  `go/cmd/eshu`
- the pipeline-status wire shape or the readiness rule. The caller decodes the
  status, evaluates it with `scan.EvaluateReadiness`, and hands this package
  the resulting `scan.ReadinessVerdict`, so the hosted and local flows cannot
  disagree about what "drained" means.
- repository-selector matching. The caller attaches a `Repository.ScopeMatch`
  predicate per entry so a `--repository` scope check resolves paths and
  symlinks the same way every other command does.

## Exported surface

`ExecuteSetup(Deps, SetupOptions) (Result, error)` and
`ExecuteOnboard(Deps, OnboardOptions) (Artifact, error)` are the two entry
points. Around them:

- `Deps` — the seams the caller wires: `Health`, `Ready`, `Readiness`,
  `ListTools`, `ListRepos`.
- `HealthzPath`, `ReadyzPath`, `StatusPath`, `ReposPath` — the deployed routes
  the seams call.
- `Result`, `Stage`, `StageName`, `StageStatus`, `FailCategory` — the staged
  outcome, plus `Result.Connected`.
- `RepoRule`, `RepoRuleKind`, `RuleVerdict`, `ParseRepoRules`,
  `ClassifyRepoRules` — rule parsing and the broad/narrow verdict.
- `Artifact`, `RuleScope`, `Connection`, `StarterPlaybook`, `StarterPrompts`,
  `StarterPlaybooks`, `ScopedIsolationLimitation` — the onboarding artifact.
- `RenderSetupHuman`, `SetupEnvelope`, `RenderArtifactMarkdown`,
  `RenderArtifactJSON`, `RenderArtifactTerminal`, `WriteArtifact`,
  `ClassifyIndexReadiness`.

`doc.go` carries the godoc contract.

## Dependencies

- `internal/cli/apierr` — `StatusCode` reads the HTTP status out of a transport
  error so the probe classifier keeps a 401/403 auth rejection distinct from an
  unreachable endpoint without naming the CLI's concrete error type.
- `internal/cli/scan` — `ReadinessVerdict`, the drain verdict the `Readiness`
  seam returns and `ClassifyIndexReadiness` consumes. Only the verdict type is
  used; the status wire shape and `EvaluateReadiness` stay in the caller.
- `internal/cli/evidredact` — `Endpoint`, the CLI-wide endpoint redaction
  applied to `APIURL`, `MCPURL`, and the URL inside `SetupSnippet`.
- `internal/cli/mcpsetup` — token redaction (`RedactToken`, `TokenReference`),
  platform resolution, and the client setup snippet. Called for pure rendering;
  `mcpsetup`'s network posture probe is never reached from here.
- `internal/query` — `PlaybookCatalog()`, a pure in-process list, is the single
  source of truth for starter prompts and playbooks.
- `internal/mcp` — `ToolDefinition` appears only as the `Deps.ListTools` return
  type. This package never calls into `internal/mcp`.

## Network and environment surface

- **Network calls this package makes: none.** Every hosted-service read goes
  through a `Deps` seam. `go/cmd/eshu`'s `hostedProbe`, `hostedReadinessVerdict`,
  and `hostedRepositoryList` own the HTTP.
- **Environment variables this package reads: none.** `ESHU_API_KEY` appears
  only as a NAME (`mcpsetup.APIKeyEnvVar`) written into output;
  `ESHU_SCOPED_TOKENS_FILE` appears only as text inside
  `ScopedIsolationLimitation`. Neither is looked up. The wrapper's
  `NewAPIClient` is what reads `ESHU_SERVICE_URL`, `ESHU_API_KEY`,
  `ESHU_REMOTE_TIMEOUT_SECONDS`, and the config file under `ESHU_HOME`, then
  passes the resolved values in as `SetupOptions`/`OnboardOptions` fields.
- **Filesystem: one call.** `WriteArtifact` calls `os.WriteFile` with mode
  `0600`, against the path the operator passed to `--out`. Nothing else here
  touches the filesystem.

The transitive import graph does pull in packages that do network and file I/O
(`internal/query` reaches storage and collector code). None of it is reachable
from the calls this package makes — `PlaybookCatalog()` returns a literal.

## Redaction: what it covers and what it does not

`Artifact` is written to disk and shared with a project team, so it is the
surface the redaction contract binds to.

Covered:

- The bearer token value never appears. `TokenSourceName` is the env var name;
  the setup snippet references `${ESHU_API_KEY}`.
- Userinfo (`user:password@`), credential-named query values, and the whole
  fragment embedded in the endpoint are stripped from `APIURL`, from the
  derived `MCPURL`, and from the URL inside `SetupSnippet` by
  `evidredact.Endpoint`. A value that does not parse as a URL is masked through
  `mcpsetup.RedactToken`.

Deliberately NOT covered:

- The endpoint's scheme, host, port, and path survive by design; an operator has
  to recognize the target. `WriteArtifact` writes `0600` for that reason.
- `Connection.Stages[].Detail` carries seam-supplied error text verbatim. A
  transport error that quotes a credential-bearing URL will reach the artifact
  through that field. `go/cmd/eshu`'s `hostedProbe` formats the endpoint into
  its message, so an operator who put a password in `--service-url` will see it
  in a failed-probe detail.
- `Result` — hosted-setup's own output — is a different contract. `TokenRef` is
  redacted, but `ServiceURL`, `SetupHint`, and stage details are verbatim. That
  output goes to the operator who owns the credential, not to a team.

`redaction_test.go` is the canary. It plants sentinels inside VALUES — an
endpoint password, a bearer token — rather than under sensitive-looking keys,
and asserts their ABSENCE from the Markdown, JSON, terminal, and on-disk
renderings, on both the connected and the refused path. A canary keyed on field
names cannot see a secret embedded in an ordinary value; that is the hole that
let the snippet leak (below).

## Gotchas / invariants

- **`Connected` is the only success signal.** A green `/healthz` and `/readyz`
  with a failing bounded query is not connected, and `nextSteps` must keep
  pointing at the first failing stage.
- **Refusal happens before any I/O.** `ExecuteOnboard` classifies rules and
  returns before touching `Deps`. `TestOnboardBroadRulesRejectedWithoutConfirm`
  fails the test if a seam is called at all.
- **An empty rule set is broad.** With the deployed `githubOrg` source mode, no
  rules means "ingest the whole org", so `nil` rules are refused like `org/*`.
- **The artifact renders its own snippet.** `buildArtifact` does not copy
  `Result.SetupHint`; it re-renders from the redacted endpoint. hosted-setup's
  snippet is built from the raw endpoint, and copying it shipped an embedded
  endpoint password to the onboarding team inside the block they are told to
  copy. `TestOnboardSnippetUsesRedactedEndpoint` is the regression.
- **`Repository.ScopeMatch` may be nil and never matches when it is.** A missing
  predicate reports the requested scope as absent rather than assuming it
  present.
- **Stage order is the contract.** `ListRepos` runs before `Readiness` so a
  failing bounded read is classified as a query failure, not as an unreachable
  status endpoint.

## Related docs

- `AGENTS.md` in this directory — scoped agent instructions.
- `go/internal/cli/mcpsetup/README.md` — the snippet and token-redaction helpers.
- `docs/public/reference/http-api.md` — the deployed routes the seams call.
