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

## Evidence table (2026-11-13, branch `drive/10653-result-shape-evidence`)

| # | Acceptance criterion | Status | Evidence |
|---|---|---|---|
| 1 | Valid Codex three-size comparison against fresh one-copy controls with a fixed background, or an arbiter waiver | OPEN — not proven | The only Codex data is the 8800 hosted run: first-matrix structured slope −1.721748 tokens/B (nonmonotonic: 84,675 / 113,874 / 61,228 input tokens over 1,153 / 7,862 / 17,284-byte fragments) and a second direct-call matrix still nonmonotonic (structured 41,726 / 29,280 / 31,878). Both matrices recorded correct four-fact answers. No third matrix exists; a live Codex client rerun is still required. |
| 2 | Manual `shape=structured` check recorded for Cursor, VS Code, OpenCode, Muse | OPEN — none run | No manual-check record exists in `docs/internal/evidence/` or the 7206 design doc (searched 2026-11-13). Setup writes `structured` only for `claude` and `codex` (`go/internal/cli/mcpsetup/setup.go`); `cursor`, `vscode`, and `generic` targets still receive `dual`. |
| 3 | Hosted week with no increase in over-budget responses, metric names and windows recorded | OPEN — on hold | On hold by owner instruction; the clock has not restarted. Metric names for the comparison: `eshu_dp_mcp_response_bytes` (now "as sent for the connection's result shape"), `eshu_dp_mcp_response_resource_fallback_total{tool}`, `eshu_dp_mcp_response_over_budget_total{tool}`, each with a `shape` attribute (`dual`, `structured`). No before/after windows recorded yet. |
| 4 | Default flip as a separate PR, `dual` still selectable, golden-corpus gate rerun at final head | CORRECTLY ABSENT | No flip diff exists by design: criteria 1–3 are open, and the issue gates the flip on all rows. Selectability is already implemented (`?shape=` per connection, `ESHU_MCP_RESULT_SHAPE` per process, default `dual` via `resultshape.OrDual` in `go/internal/mcp/dispatch_envelope.go`). |
| 5 | `eshu mcp setup` decision recorded for Cursor, VS Code, generic | OPEN — gated on criterion 2 | No decision recorded; the manual checks it depends on have not run. |

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

## Code state verified 2026-11-13 (origin/main `0148ce2b3a6`)

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
