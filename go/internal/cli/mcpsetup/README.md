# MCP Setup

## Purpose

`mcpsetup` owns the business logic behind `eshu mcp setup` (and its `eshu m`
alias): rendering a platform-specific MCP client snippet, resolving which
auth posture to wire (per-user token, SSO, or the legacy shared key),
merging the eshu server entry into an existing client config file, and
running the staged `--verify` reachability checks.

## Ownership boundary

This package owns setup *logic* -- what to render and what to check. It does
not own process wiring: reading cobra flags, resolving `*APIClient` from
environment/config, or printing to stdout/stderr and mapping errors to exit
codes. Those stay in `go/cmd/eshu/mcp_setup_cmd.go`, the cobra `RunE`
wrapper, because `go/cmd/eshu` is `package main` and nothing can import it.
The wrapper resolves process state and passes it into this package as plain
values or as the `HealthProber`/`QueryProber` interfaces below; this package
returns data and errors, never printing anything itself.

## Exported surface

- `SetupRequest`, `SetupMode`/`ModeLocalStdio`/`ModeHostedHTTP` -- the
  resolved inputs to snippet generation
- `Platform`, `ResolvePlatform`, `SupportedPlatformNames` -- the supported MCP
  client registry (claude, cursor, vscode, codex, generic)
- `RenderSetupSnippet` -- renders the full guidance block (snippet, target
  file, posture-specific credential guidance)
- `AuthPosture`/`PostureToken`/`PostureSSO`/`PostureSharedKey`,
  `PostureProbeResult`, `ResolveAuthPosture`, `HostedPostureProbe` -- the RFC
  9728 discovery probe and posture resolution (auto/sso/token/shared-key)
- `RedactToken`, `TokenReference` -- display-safe credential handling; neither
  ever returns a raw secret
- `WriteMCPServerConfig`, `DefaultWriteTarget`, `DescribeWriteTarget` -- the
  `--write` merge path
- `VerifyStage`/`StageResult`/`VerifyReport`/`RunVerification`/
  `RenderVerifyReport`, `HealthProber`, `QueryProber` -- the staged
  `--verify` reachability/tools/first-query checks. `HealthProber` and
  `QueryProber` are the seam go/cmd/eshu implements over `*APIClient`
  (`apiHealthProber`, `apiQueryProber` in mcp_setup_cmd.go), so this package
  never depends on the CLI's process-level client type.
- `APIKeyEnvVar`, `MCPTokenEnvVar` -- the env-var names the hosted-mode
  snippets reference for the shared admin/dev key and the per-user token

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/mcp` -- `mcp.ToolDefinition` and `mcp.ReadOnlyTools`, used by the
  tools-visible verification stage
- Consumed by `go/cmd/eshu`: the `mcp setup`/`m` wrapper
  (`mcp_setup_cmd.go`), the `hosted-setup`/`hosted-onboard` commands (which
  reuse `TokenReference`/`RedactToken`/`RenderSetupSnippet` to show the
  credential they actually used), `assistant.go` (reuses the local
  stdio verification seam for `assistant status --verify`), and
  `first_run_evidence.go` (reuses `RedactToken` for endpoint redaction)

## Telemetry

None. Setup and verification run inline with the CLI invocation and print a
terminal report; there is no background pipeline stage to instrument.

## Gotchas / invariants

- Never embed a raw secret in rendered output. Hosted snippets reference a
  credential by env-var name (`${ESHU_MCP_TOKEN}` / `${ESHU_API_KEY}`) for
  platforms that support it; platforms that do not get a masked placeholder
  via `RedactToken`/`TokenReference`, never the value.
- `AuthPosture`'s zero value is `PostureToken` deliberately: any call site
  (including a test) that forgets to set `SetupRequest.Posture` gets the safe
  per-user-token default, never the shared key.
- `--verify`'s query stage must exercise the SAME credential the emitted
  snippet wires (token vs. shared-key vs. skipped for SSO). That
  posture-to-credential decision reads process env
  (`ESHU_MCP_TOKEN`) and stays in `mcp_setup_cmd.go`'s `mcpSetupVerify`, not
  in this package -- this package only runs whatever `HealthProber`/
  `QueryProber` it is handed.
- The RFC 9728 discovery probe (`HostedPostureProbe`) uses a dedicated
  3-second-timeout `*http.Client`, never the `*APIClient`'s 30s default, so an
  offline `--hosted` run fails fast instead of hanging.

## Related docs

- `docs/public/operate/mcp-client-auth.md` -- the client-auth doc whose
  literals `TestDocLockstepLiterals` (doc_lockstep_test.go) pins against this
  package's actual rendered output
- `docs/public/mcp/index.md` -- documented client-side MCP setup contract
