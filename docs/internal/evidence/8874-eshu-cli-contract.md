# #8874 eshu-cli contract (extraction step 2)

Refs #8874, part of #6707. Status: preparation only. Nothing in this
record has been pushed to the destination repository.

## Scope

Base: `origin/main` including the merged step-1 census (#9361). That
record inventories the commands, flags, environment variables, and
output/exit contracts and classifies every visible command as EXTRACT,
RETAIN, RUNTIME, or STUB with zero DECIDE rows. This record is the
versioned day-one contract `eshu-hq/eshu-cli` publishes: the supported
core<->CLI version pairs, credential handling, demo asset distribution,
and the local-host entrypoint boundary. The base is stated without a
fixed SHA so routine rebases cannot stale it; the contract tests re-pin
every figure to the tree on each run.

This record moves no code and authorizes no release, deployment,
rollback, or in-tree removal. The destination repository
`eshu-hq/eshu-cli` holds a README only at `67b92b6`, so criteria covering
independent release, rollback rehearsal, and production acceptance
remain unmet by construction.

Migration stage: `planned`, per
`docs/internal/design/eshu-ecosystem-repository-migration.md`
(`eshu-cli` owns user-facing CLI commands and their supported API/MCP
clients; core-only bootstrap and repair stay in `eshu`).

## Extraction surface

`eshu-cli` publishes the census EXTRACT/STUB families unchanged in
shape: every user-facing command in `go/cmd/eshu/` except the RETAIN
core-only repair family (`admin`), the RUNTIME launchers (`api start`,
`mcp start`, `serve start`, hidden `local-host`), and the RETAIN local
verbs (`graph`, `watch`, `w`) the arbiter ruling keeps with the
supervisor. What moves is the cobra tree plus the `go/internal/cli`
families it owns (admin excluded), `client.go` with the envelope MIME
`application/eshu.envelope+json`, `output.go`, the `contract.go` exit
table, and the env-registry dependency. RUNTIME launchers stay out, and
the destination repoints imports from `go/internal/...` to its own
module or the published SDK surface, never to core implementation
handles. Parity proof before any destination PR stays as the census
records it: representative plus hostile inputs across `query`, `map`,
`trace`, `change`, `scan`, `report`, `service-report`, and
`vuln-scan provider-parity`.

## Version basis

The CLI reports `buildinfo.AppVersion()`: linker-injected release
versions win, `go install` module versions second, source builds print
`dev` (`eshu --version` prints `Eshu <buildinfo.AppVersion()>`).
Component-to-core compatibility pins through `--core-version` and
`ESHU_COMPONENT_CORE_VERSION`, honored since the component core-version
fix: a CLI install verifies the manifest against the core release it
talks to instead of ignoring the pin.

Wire parity is the envelope MIME plus the apierr status crossing plus
the shared exit table in `trace.go` (`traceExitCode`): `0` success,
`1` generic, `2` invalid argument / not found / scope errors, `3`
ambiguous, `4` index-building / stale, `5` degraded / partial,
`6` unsupported capability. Removed commands keep failing through
`removedCommandError` with replacement guidance, never silently.

## Compatibility matrix

Single tested pair at extraction (v0). This compatibility matrix
converges oldest and newest on that pair; independent releases widen
the range only with tested pairs recorded here.

| Component | Oldest supported | Newest tested |
| --- | --- | --- |
| eshu core (same-tree build) | same-tree `Eshu <buildinfo.AppVersion()>` | same-tree `Eshu <buildinfo.AppVersion()>` |
| eshu-cli (same-tree build) | same-tree `Eshu <buildinfo.AppVersion()>` | same-tree `Eshu <buildinfo.AppVersion()>` |
| Component manifests | `--core-version` / `ESHU_COMPONENT_CORE_VERSION` equal to the core release | as oldest |
| Demo overlay | `docker-compose.demo.yaml` with `ESHU_DEMO_COMPOSE_FILE` seam | as oldest |
| Demo manifest | `specs/demo-first-answers.v1.yaml` | as oldest |

The demo CLI<->Compose<->core-image version triple enters the
#8871 matrix; this record carries the CLI side of that seam.

## Credential handling

No credential material lives in this repository or in the destination
content. The CLI resolves `ESHU_API_KEY` in flags -> profile
(`cliconfig.ResolveValue`) -> env -> default order and sends it as an
`Authorization: Bearer` header; `config validate` checks every `ESHU_*`
value against the central registry in `go/internal/cli/config`.
`mcp setup` wires the per-user `ESHU_MCP_TOKEN` Bearer [REDACTED] reference
into IDE snippets and verifies posture without persisting the secret.
Component trust falls back to `ESHU_COMPONENT_TRUST_MODE`,
`ESHU_COMPONENT_ALLOW_IDS`, `ESHU_COMPONENT_ALLOW_PUBLISHERS`,
`ESHU_COMPONENT_REVOKE_IDS`, `ESHU_COMPONENT_REVOKE_PUBLISHERS`, and
`ESHU_COMPONENT_CORE_VERSION` when the matching flags are absent.
Evidence paths redact through `go/internal/cli/evidredact`; the demo
passes its ephemeral key through the exec environment seam, never the
process environment. Vulnerability response ownership for the extracted
stream is NOT_CHECKED: `eshu-cli` must name it before its first
independent release.

## Demo asset split

The `demo` command (`up`, `status`, `down` plus `demo-benchmark`, a
clean API/MCP client showcasing CLI UX) moves to `eshu-cli`. The
Compose assets (`docker-compose.demo.yaml` and fragments), the question
manifest pin (`specs/demo-first-answers.v1.yaml`), and the tested
CLI<->Compose<->core-image triple move to `eshu-deploy` under #8871.
The cross-repo seam is `ESHU_DEMO_COMPOSE_FILE`: an operator-set
override wins outright and the value is handed to `docker compose -f`,
never opened by this process; otherwise `ResolveComposeFile` walks up
from the working directory for `docker-compose.demo.yaml`, so an
installed binary works outside a checkout, and a miss fails with
guidance naming `ESHU_DEMO_COMPOSE_FILE`. The demo project default is
`eshu-demo`, deliberately distinct from any real-work stack. The
manifest path `specs/demo-first-answers.v1.yaml` stays the committed
acceptance oracle for the five demo questions.

## Local-host entrypoint

`local-host` is hidden and stays in `eshu`: registration and signal
handling only, with the supervisor in
`go/internal/cli/localsupervisor`. It exposes `watch` and `mcp-stdio`
subcommands; `graph start` and `watch` re-exec the exact argv
`eshu local-host watch <workspace-root>` through `procexec`. The name,
versioning, and discovery of a future versioned core `local-host`
entrypoint that would let `graph`/`watch` verbs appear in `eshu-cli`
is NOT a cross-repo contract yet, so `graph`, `watch`, and `w` stay
RETAIN until that entrypoint exists and is pinned here.

## Install filesystem and source contract

`install nornicdb` moves to `eshu-cli` with the fail-closed source
policy and the managed-home filesystem seam unchanged (census `install`
row, EXTRACT). `graph upgrade` accepts the same `--from`/`--sha256`
source flags but stays in core under the census RETAIN `graph` row
(sidecar lifecycle); it does not move in this cutover. The
managed home resolves from `ESHU_HOME` when set (leading `~` expanded per
`doc.go`/`layout.go`), otherwise per platform; it is not `~/.eshu` on any
of them. The installed binary name
is `nornicdb-headless`, installed at `<home>/bin/nornicdb-headless`,
and the install manifest is `<home>/graph-backends/nornicdb/manifest.json`.
Headless stays the default laptop artifact; full binaries install only
through an explicit `--from` source. The version probe is `<binary> version`
parsing `NornicDB <version>`: `graphinstall` never executes a binary itself,
so `eshu-cli` supplies its own `VersionReader` (wired here to `ReadGraphVersion`
in `localsupervisor`; a nil reader is rejected before any work). Remote
`http`/`https` sources require `--sha256` with the independently verified
artifact checksum: `remote nornicdb sources require --sha256`. `--full` with
`--from` is rejected: `--full is reserved for future no-argument release installs`.
The cobra flags and
exit-code mapping stay in `go/cmd/eshu` (`package main`,
`runInstallNornicDB`) while verification and install live in
`go/internal/cli/graphinstall`; both move together, never repointed at
core implementation handles. The supported CLI<->core-host pair enters
the #8871 matrix; this record carries the CLI side of that seam.

## Cutover readiness

`go/cmd/eshu/cli_extraction_contract_test.go` pins every section above
to the tree: record anchors, demo seam figures, envelope MIME and exit
table, live Bearer [REDACTED] and Accept headers, compose-seam override
and miss guidance, the hidden `local-host` boundary with its `watch`
and `mcp-stdio` subcommands, the install filesystem seam with its
fail-closed source policy and version-reader boundary, and non-empty
version reporting. The
cutover PR repoints the destination at these figures, proves parity per
the census plan, and removes the in-tree originals only after
independent build, release, deploy, and rollback rehearsal with
production acceptance recorded.

NOT_CHECKED: independent release, QA EKS deployment, rollback
rehearsal, production acceptance, and vulnerability-response ownership
for the extracted stream. No dest-repo push and no in-tree removals in
this lane.
