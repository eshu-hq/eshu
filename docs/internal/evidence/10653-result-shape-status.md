# MCP one-copy structured default: consolidated evidence table (#10653)

Scope: evidence only. This file consolidates the current proof state for the
five #10653 acceptance criteria so the default-flip decision has one table to
close. It changes no runtime default: the process default remains `dual`, and
no satellite or in-tree flip diff exists in this lane.

Prior work it builds on (not redone):

- [Structured result-contract proof](8800-structured-result-contract.md) —
  strict structured Neo4j golden-corpus run, 629 checks, 0 required failures,
  at `9a43126e3f56aeb2c53814b9e6da913374619b5c`.
- [Hosted result-shape measurement](8800-hosted-result-shape-measurement.md) —
  2026-10-26 client-input run, Claude slope proven, Codex slope not proven.
- [Budget fallback design](../design/7206-mcp-response-budget-fallback.md) —
  decision note records the opt-in rollout and the unimplemented flip.

## Evidence table (2026-11-14, branch `drive/10653-result-shape-evidence`)

| # | Acceptance criterion | Status | Evidence |
|---|---|---|---|
| 1 | Valid Codex three-size comparison against fresh one-copy controls with a fixed background, or an arbiter waiver | PARTIAL — structured proven (1.005×), dual still non-monotonic | Third matrix, 2026-11-14 (Codex CLI 0.167.0, model `gpt-5.6-luna`, low effort, `--ephemeral`, fixed background, fresh one-copy controls): control 24,719 / 27,118 / 29,835 input tokens over 1,353 / 10,075 / 19,947-byte fragments (slope 0.2751 tokens/B, monotonic); structured 49,523 / 51,895 / 54,662 (slope 0.2765, **ratio 1.005**, monotonic); dual 49,861 / 80,126 / 59,670 (**non-monotonic**, cached-input variance). The structured series is a valid three-size comparison against fresh controls and matches one copy; the dual series is non-monotonic for the second time, so the dual token slope is still not established from Codex input tokens (the wire bytes prove dual ≈ 2.1× — see below). |
| 2 | Manual `shape=structured` check recorded for Cursor, VS Code, OpenCode, Muse | PARTIAL — 1 of 4 run | OpenCode v2.3.0 (2026-11-14): `call_tool`(`list_indexed_repositories`, `limit` 12) over `?shape=structured` returned one-copy `structuredContent` and the model reported the correct four facts `{returned_count:12, overall_total:820, truncated:true, freshness:"fresh"}` — PASS. Muse 1.4.20: NOT_CHECKED (MCP server configured and enabled, but the Meta provider subscription quota is exhausted — HTTP 429, resets 2026-11-16 — so no model call completed). Cursor: NOT_CHECKED (no Cursor IDE installed; only the `cursor agent` fallback, no graphical result-render check). VS Code: NOT_CHECKED (`code` CLI not on PATH). |
| 3 | Hosted week with no increase in over-budget responses, metric names and windows recorded | ON HOLD — post-flip per arbiter | Re-framed by the arbiter ruling (2026-11-14): the one-week hosted observation runs **once after the flip diff deploys**, not before it. Still on hold by owner instruction; do not fabricate a window. Metric names for the comparison: `eshu_dp_mcp_response_bytes` (now "as sent for the connection's result shape"), `eshu_dp_mcp_response_resource_fallback_total{tool}`, `eshu_dp_mcp_response_over_budget_total{tool}`, each with a `shape` attribute (`dual`, `structured`). No before/after windows recorded yet. |
| 4 | Default flip as a separate PR, `dual` still selectable, golden-corpus gate rerun at final head | CORRECTLY ABSENT | Arbiter ruling (2026-11-14): the flip applies to **both** the satellite and the core in-tree layer; a failing client keeps `dual` from setup while healthy clients flip (per-client). Still no flip diff in this lane: criterion 1's dual series and criterion 2's three unrun clients are open. Selectability is already implemented (`?shape=` per connection, `ESHU_MCP_RESULT_SHAPE` per process, default `dual` via `resultshape.OrDual` in `go/internal/mcp/dispatch_envelope.go`). |
| 5 | `eshu mcp setup` decision recorded for Cursor, VS Code, generic | ANSWERED — per-client | Arbiter ruling (2026-11-14): a failing client keeps `dual` from `eshu mcp setup` while healthy clients flip to `structured` (per-client, no serialization on the slowest client). OpenCode is a `generic`-target client and passed its check; the concrete per-platform wiring for Cursor, VS Code, and generic is deferred until their manual checks (criterion 2) are complete. |

## What is already proven (do not re-prove)

- **Correctness under `structured`.** The strict structured golden-corpus run
  passed 629 checks with zero required failures
  ([contract proof](8800-structured-result-contract.md)), and every captured
  MCP cell in the hosted run answered the four requested facts (returned
  count, overall total, truncation flag, truth freshness) correctly under both
  shapes for both clients.
- **Claude dual-vs-structured parity (one configuration).** Three-size
  structured/control slope ratio **0.999903** (0.4271805348 vs 0.4272218501
  tokens/B); dual slope 0.8546778206 tokens/B, about twice the control. Tested
  configuration only: Claude Code CLI 2.1.310, model `claude-sonnet-5-5`,
  `list_indexed_repositories` at three fixed pages, `MAX_MCP_OUTPUT_TOKENS`
  override. Not a general token-saving claim.
- **Budget accounting is shape-aware.** The 262,144-byte limit is unchanged and
  measures the exact bytes sent in the connection's shape. Under `structured`
  there is no second copy, so the resource-only fallback never applies and
  `ResourceOnly` is never set; results up to 256 KiB pass untouched.
- **Wire cost of `dual`.** A 67,921-byte service-story payload arrived as an
  about 146 KB tool result because `dual` carries the payload twice (reported
  in #10653 from the QA environment, which serves `dual`).
- **Measured wire slope (2026-11-14, QA satellite `eshu-mcp` v0.2.15).**
  `list_indexed_repositories` at limit 1/12/24 returns `structuredContent`
  fragments of 1,353 / 10,075 / 19,947 bytes. OLS fit of total tool-result
  bytes against fragment bytes: `structured` slope **1.000053** (1,450 /
  10,173 / 20,045 result bytes), `dual` slope **2.101445** (3,086 / 21,433 /
  42,161 result bytes). `get_service_story` (`api-crm-backend`, one live
  service) confirms the same ratio: `structured` ≈ 1.007–1.009×, `dual` ≈
  2.11× (handles 15,229 vs 31,912; full 18,283 vs 38,282 result bytes). This
  is the physical basis for the token claim: the wire carries one copy under
  `structured` and two under `dual`.

## Arbiter ruling (2026-11-14, binding)

1. The default flip applies to **both** the satellite and the core in-tree
   layer.
2. A client that fails its manual check keeps `dual` from `eshu mcp setup`
   while healthy clients flip — per-client, no serialization on the slowest
   client.
3. The one-week hosted observation runs **once after** the flip diff deploys;
   record as on hold per owner instruction, do not fabricate.
4. Mirror the satellite's `#74` candidate-ids and `#73` structured-refusal
   behavior into `go/internal/mcp` (the in-tree copy) with a parity test. This
   lane is docs-only, so that mirroring is reported as needing its own lane
   rather than landed here.

## Code state verified 2026-11-14 (origin/main `76240fda9f`)

- `go/internal/mcp/dispatch_envelope.go`: `WithResultShape` folds through
  `resultshape.OrDual`, so any unset or unparsable value is `dual`. Rendering
  lives in `renderToolResult`, defined in `go/internal/mcp/dispatch_envelope.go` (called from `go/internal/mcp/server.go`), via the envelope
  renderers in `dispatch_envelope.go`.
- `go/internal/cli/mcpsetup/setup.go`: only the `claude` and `codex` platform
  snippets mention the one-copy structured shape; `cursor`, `vscode`, and
  `generic` do not.

## Negative-search record

2026-11-13, from the worktree root at `0148ce2b3a6`:

- `rg -il "cursor|vscode|opencode|Muse"` over the 8800 measurement file and
  the 7206 design doc: no hits — no manual client checks recorded.
- `rg` for hosted-week comparison content (`over-budget` windows, before/after
  metric series) across `docs/internal/evidence/` and `docs/internal/design/`:
  only the requirement statements in the two files above — no data.
- `rg` for newer Codex token-slope evidence: the only result-shape Codex data
  is the 8800 file's two NOT_PROVEN matrices; other `codex` hits in
  `docs/internal/evidence/` are unrelated topics.
