<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# D13 row-2 leaf-2b drain evidence

No-Regression Evidence (#9669): `go/internal/mcp` Groups A/D/B off the
in-tree `query` root (row-2 leaf 2b drains the last two Group C files,
`router/access.go` and `transport_auth.go`, at `query/auth`): the Group C
auth ledgers (shared-key-only + pending-row-filtering maps, the
scoped-token allowlist decision with all family matchers, the
browser-session/all-scope route policy with both denial reasons, and the
route-denial signal) move from root package `query` to `query/auth` with
byte-identical bodies (modulo the package line, the dropped #6818
`//nolint:dirgate` trailer, and the export-case renames Go visibility
requires: 3 maps, 2 denial-reason funcs, 1 predicate, 1 mark func,
2 reason codes, 8 test-pinned family matchers). Root keeps same-named
forwarders plus the `BrowserSessionRoutePolicy` alias so the middleware,
the root tests, and the MCP exhaustiveness gate read the same values with
no duplicated ledger; the middleware constructors, the advertised-ledger
and class taxonomy, and the audit emitters stay in root. `AuthContext` /
`AuthMode*` retarget at the already leaf-homed `query/auth/context.go`.
`TestQueryRootDrained` now names zero exceptions and asserts the offender
set equals the allowed set (P3 hardening: a stale exception reds the same
way a new offender does). `go build ./...`, `go vet ./...` clean;
`go test ./internal/mcp/... ./internal/query/...` green;
`TestScopedTokenAllowlistCompleteness` and
`TestEveryMCPReachableRouteIsScopedOrAnnotated` green with zero assertion
edits; the drop-one-allowlist-entry mutation probe reds
`TestScopedTokenAllowlistCompleteness` as required; transport-auth denial
classification tests green; the #6818 nolint is resolved on the moved
files, not carried. Query-auth file count 7 -> 30, still under the
dirgate 40 cap; query root shrinks; no baseline edits.

No-Observability-Change (#9669): no telemetry added, removed, or renamed;
no new spans, metrics, or log paths. The
`eshu_dp_mcp_transport_auth_denied_total` reason labels are unchanged.
