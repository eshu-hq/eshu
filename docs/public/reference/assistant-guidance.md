# Assistant Guidance Install

`eshu assistant` writes project-scoped instructions that tell AI assistants to
prefer Eshu's bounded MCP/API tools for graph-backed questions and to respect
Eshu truth labels, freshness, and missing evidence. Use it once per project so
Claude Code, Codex, Cursor, and compatible clients reach for Eshu before broad
raw-file search.

This is separate from `eshu mcp setup`, which configures an MCP client to launch
the Eshu server. `eshu assistant` only manages instruction files in your repo.

## Install Contract

The assistant ritual install contract is `assistant_ritual_install.v1`. It is a
project guidance contract, not a source-code mutation hook or a background
agent. The command may write only the platform guidance files listed below, and
only after the user runs an install command or chooses an equivalent explicit
write action in a host integration.

By default the ritual is read-only:

- It instructs assistants to ask Eshu first for graph-backed code, deployment,
  infrastructure, and documentation questions.
- It does not install hooks, intercept editor actions, mutate source files,
  change Git state, start long-running daemons, or add secrets.
- It does not embed private endpoints, bearer tokens, local absolute paths, or
  machine-specific owner ports in committed guidance.
- It reserves any PreToolUse, editor hook, or fast-path interception for a
  follow-up implementation governed by the
  [Assistant Fast-Path Hook Contract](assistant-fast-path-hooks.md), with
  opt-in enablement, a latency budget, and proof that hot paths never run
  unbounded graph reads.

The installed text is allowed to describe how to discover Eshu and how to use
bounded MCP/API reads. It must not hard-code a specific user's endpoint or
token.

## Commands

| Command | Effect |
| --- | --- |
| `eshu assistant install` | Write or refresh the Eshu guidance block for every supported assistant. |
| `eshu assistant install --verify` | Write or refresh guidance, then run the safe local ritual diagnostics. |
| `eshu assistant status` | Report, per platform, whether the guidance block is installed and current. |
| `eshu assistant status --verify` | Add first-run ritual diagnostics for guidance state and local MCP tool visibility. |
| `eshu assistant uninstall` | Remove the Eshu guidance block, preserving other file content. |

Flags:

- `--path <dir>` operates on the given project root instead of the current
  directory.
- `--platform <id>` restricts the action to one assistant: `claude`, `codex`, or
  `cursor`. An unknown id is rejected.
- `--verify` on `install` and `status` runs the safe local ritual diagnostics
  for the selected platform set. It does not write MCP client config, start
  services, install hooks, query the graph/API, or print secrets.

## Target Files

| Platform | File | Committed |
| --- | --- | --- |
| Claude Code | `CLAUDE.md` | yes |
| Codex / AGENTS.md | `AGENTS.md` | yes |
| Cursor | `.cursor/rules/eshu.mdc` | yes |

Per-host boundaries:

- Claude Code guidance lives in the project `CLAUDE.md`. MCP server setup stays
  in Claude's MCP configuration and is owned by `eshu mcp setup`, not this
  command.
- Codex guidance lives in project `AGENTS.md`. Repo-local guidance is not a
  substitute for proving the active Codex MCP server is visible in the current
  session.
- Cursor guidance lives in `.cursor/rules/eshu.mdc`. Cursor MCP configuration
  remains separate and must be verified by the client after install.

If a target file already exists, `install` may update only the managed block. If
the platform is unsupported, the command rejects that platform without guessing a
target file. If a target cannot be read or written, the command reports the
error and stops; platform files successfully updated earlier in the same run may
remain updated, and platform files not yet processed remain untouched.

## Endpoint Discovery

Guidance files point assistants at the current Eshu discovery order instead of a
literal private endpoint:

1. Use the MCP client entry written by `eshu mcp setup` when the active
   assistant session exposes it.
2. Use the user's configured hosted endpoint variables, such as `ESHU_MCP_URL`
   and `ESHU_MCP_TOKEN`, only as references; never copy their values into
   committed guidance.
3. For local development, rely on `eshu first-run`, `eshu hosted-setup`, or
   `eshu mcp setup --verify` to prove the API/MCP endpoint, token, and owner
   process before asking the assistant to query Eshu.

Generated guidance should tell assistants to start from a narrow canonical
scope (`repo_id`, service, environment, file, symbol, workload, or resource) and
to honor pagination and truncation metadata. Whole-graph questions are not a
valid first call when a narrower scope is known.

## Managed Block

Guidance lives inside a clearly delimited managed block between
`<!-- BEGIN ESHU GUIDANCE -->` and `<!-- END ESHU GUIDANCE -->`. Install and
reinstall rewrite only the bytes inside those markers, so any content you keep
before or after the block is preserved. Reinstall is idempotent: running it
again with the same guidance leaves the file byte-for-byte identical.

`uninstall` removes only the managed block and its markers. It deletes a file
only when that file held nothing but the Eshu block (so Eshu effectively created
it). A file with your own content keeps that content with just the block
stripped, and a file Eshu did not create is never deleted.

## Generated Guidance

The block instructs assistants to:

- Call the bounded Eshu tool first for graph-backed questions and fall back to
  raw-file search only when no Eshu tool fits.
- Keep calls bounded with the narrowest known `repo_id`, service, environment,
  resource, file, or symbol, and to honor `truncated`, `next_offset`, and
  `next_cursor`.
- Start from concrete first prompts such as building a service story or tracing a
  deployment chain.
- Respect Eshu truth labels: read `truth.level` (exact, derived, fallback) and
  `truth.freshness.state` (fresh, stale, building, unavailable), and state when
  evidence is missing rather than inventing graph edges.

See the [MCP Guide](../guides/mcp-guide.md),
[Starter Prompts](../guides/starter-prompts.md), and
[Truth Label Protocol](truth-label-protocol.md) for the canonical language the
guidance references.

## First-Run Diagnostics

An assistant ritual install is not complete just because the files exist. The
first run should prove, in order:

1. The expected guidance block is installed for the requested platform.
2. The active assistant session can see the Eshu MCP server or the configured
   API/MCP endpoint.
3. Authentication is available without exposing the raw token in output.
4. The selected repository scope resolves to indexed Eshu state.
5. Index freshness is usable: `fresh` for normal answers, or an explicit
   `building`, `stale`, or `unavailable` status that the assistant must report.
6. One bounded first query is planned with a scope and `limit`; if it runs, the
   response must include truth and freshness metadata.

Failure is safe and visible:

| Failure | Required behavior |
| --- | --- |
| Unsupported assistant | Report the platform as unsupported; do not write a best-guess file. |
| Missing MCP server | Leave guidance installed but report Eshu unavailable until MCP setup is verified. |
| Missing token or endpoint | Report the missing reference; never write the raw secret or private URL into guidance. |
| Stale or building index | Tell the assistant to surface the freshness state instead of answering as current. |
| Ambiguous repository scope | Ask for a narrower repo, service, file, symbol, workload, or resource selector. |
| Broad hot-path prompt | Refuse an unbounded graph read and choose a summary/count/handle tool first. |

`eshu assistant status --verify` implements the local guidance portion of this
checklist. It keeps the normal per-platform status table, then reports whether
the selected guidance blocks are current, whether local MCP setup text can be
generated, whether the in-process read-only MCP tool surface is visible, and
whether endpoint and first-query probes are skipped because the local-stdio path
has no HTTP endpoint to probe. `eshu assistant install --verify` runs the same
safe report after a successful install/update/no-op, using the same selected
platform set. Hosted endpoint and authentication proof remain owned by
`eshu mcp setup --verify`, `eshu first-run`, and `eshu hosted-setup`.

The verification mode does not start a hook, mutate MCP client configuration,
run a broad graph query, or print token values.

No-Regression Evidence: this contract is docs-only. It adds no API route, MCP
tool, graph query, worker, queue, reducer, hook, or installer behavior.

No-Observability-Change: runtime diagnostics still use the existing MCP setup,
first-run, hosted-setup, truth envelope, readiness, and telemetry signals; no
metric, span, log, or status field changes in this contract.

## Committing Guidance

After an install that changed a committed file, the command prints `git add`
hints so teammates and CI agents share the same guidance:

```text
Commit the guidance so teammates and CI agents share it:
  git add CLAUDE.md
```

## Related Docs

- [CLI Reference](cli-reference.md)
- [MCP Guide](../guides/mcp-guide.md)
- [Starter Prompts](../guides/starter-prompts.md)
- [Truth Label Protocol](truth-label-protocol.md)
- [Assistant Fast-Path Hook Contract](assistant-fast-path-hooks.md)
