# Assistant Fast-Path Hook Contract

The assistant fast-path hook contract is `assistant_fast_path_hook.v1`. It
describes an optional, opt-in assistant integration that may surface bounded
Eshu context before a user or assistant performs broad source exploration such
as Read, Grep, Glob, file search, or editor symbol lookup.

This page is a contract gate. It does not ship a hook, host adapter, MCP call,
API route, cache, graph query, or source mutation.

## Supported Host Boundary

A host is supported only when its documented extension point can enforce the
full contract below. Eshu must not infer hook support from a similarly named
feature or from another host.

| Host family | Boundary |
| --- | --- |
| Claude Code-style PreToolUse | Eligible only after an opt-in hook can observe the candidate tool name and bounded input without mutating the tool request. |
| Codex | Guidance-only until the active Codex environment exposes an equivalent documented hook with bounded input, timeout, and read-only output. |
| Cursor | Guidance-only until Cursor exposes an equivalent documented hook or rules integration that can run the same bounded preflight without changing source. |
| Other MCP clients | Guidance-only unless the client documents a compatible pre-tool or pre-search extension point. |

Unsupported hosts continue to use [Assistant Guidance](assistant-guidance.md)
and [Connect MCP](../mcp/index.md). They must not install best-guess hooks.

## Trigger Classes

Fast-path hooks may consider only exploration-shaped triggers:

- source file read
- text or symbol search
- glob/file discovery
- editor symbol lookup
- prompt-visible requests for blast radius, deployment chain, service story, or
  infrastructure ownership

The hook must not run for write, edit, format, delete, commit, push, shell,
secret-management, provider, or deployment commands. If a trigger is ambiguous,
the safe outcome is no hook output.

## Bounded Query Shape

Every hook-planned Eshu read needs:

- a canonical scope: `repo_id`, repo-relative path, entity ID, service,
  workload, environment, or resource handle
- an explicit `limit`
- deterministic ordering
- server-side timeout or cancellation
- visible `truth.level`, `truth.profile`, `truth.freshness.state`, and
  truncation or continuation metadata

The first call should be a cheap summary, count, handle, or story-status read.
It must not fetch whole files, whole graphs, broad relationship expansions, or
large source bodies on the hot path. If the trigger cannot supply a narrow
scope, the hook should emit no context and should tell the assistant to ask a
bounded Eshu question explicitly.

## Latency Budget

The default hook budget is 200 ms wall time for local hot-path preflight. A host
adapter may choose a lower budget, but it may not exceed 200 ms without a
tracked benchmark showing that the host remains usable.

When the budget expires, the hook must fail open: allow the original tool or
editor action to continue and report `eshu_hook_timeout` only as optional
diagnostic context. Timeout must not block reads, edits, commits, or searches.

Before any implementation ships, measurement evidence must record:

- host and hook family
- Eshu runtime profile
- trigger class
- cache state
- query or no-query decision
- p50, p95, and max wall time
- timeout count and fallback count
- redaction and publish-safety result

## Cache And Freshness

Hook caches are optional and must be process-local or user-local, never
committed. Cache keys must avoid raw absolute paths, tokens, private endpoints,
and provider identifiers. Use safe handles such as repository ID,
repo-relative path, entity ID, query family, and freshness state.

A cache hit may suppress a repeat Eshu call only when it preserves truth and
freshness metadata. A stale, building, unavailable, or truncated result must
remain visible as degraded context; the hook must not restate it as current or
complete.

## Output Shape

Hook output is advisory context, not canonical truth. It may include:

- the Eshu tool or endpoint family that would answer the question
- the narrow scope selected
- a short result summary with truth and freshness labels
- missing evidence, truncation, timeout, or unsupported-host reason codes
- the next bounded Eshu call the assistant should make

It must not include raw tokens, private endpoints, private hostnames, local
absolute paths, private addresses, raw provider payloads, prompt bodies,
provider responses, or large source excerpts.

## Safe Failure Modes

| Failure | Required behavior |
| --- | --- |
| Unsupported host | Do not install or run a hook; use guidance-only mode. |
| Hook not explicitly enabled | Do nothing beyond installed guidance. |
| MCP server hidden or unavailable | Do not retry broad calls; report `eshu_mcp_unavailable` if the host supports diagnostic output. |
| Missing endpoint or token reference | Report the missing reference without printing its value. |
| Broad or missing scope | Emit no query context and ask for a narrower repo, file, symbol, service, workload, environment, or resource. |
| Stale or building index | Surface the freshness state and avoid current-truth claims. |
| Timeout | Fail open and report `eshu_hook_timeout`. |
| Permission denied | Report `eshu_permission_denied` without exposing scopes, tokens, or private resource names. |

## Implementation Gate

No fast-path hook may ship, default on, or claim support until a follow-up PR
adds implementation proof for the target host:

- opt-in configuration and uninstall path
- failing tests first for unsupported host, missing endpoint, broad scope,
  timeout, stale index, permission denial, and unsafe output
- bounded MCP/API proof with timeout, limit, deterministic ordering, truth,
  freshness, and truncation metadata
- latency measurement against the 200 ms budget
- publish-safety scan proving private values are not emitted or committed
- `No-Regression Evidence:` for any runtime path touched
- `Observability Evidence:` or `No-Observability-Change:` in tracked docs

No-Regression Evidence: this contract changes documentation only. It adds no
hook, host adapter, API route, MCP tool, graph query, worker, queue, reducer,
cache, or installer behavior.

No-Observability-Change: future hook implementations must define their own
diagnostic signals. This contract uses existing MCP setup, first-run,
hosted-setup, truth envelope, readiness, and telemetry docs without changing
metrics, spans, logs, status fields, or pprof output.

## Related Docs

- [Assistant Guidance](assistant-guidance.md)
- [Connect MCP](../mcp/index.md)
- [MCP Guide](../guides/mcp-guide.md)
- [Truth Label Protocol](truth-label-protocol.md)
