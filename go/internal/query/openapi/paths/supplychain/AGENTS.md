# Supply-Chain OpenAPI Path Fragments — Agent Instructions

Scope: `go/internal/query/openapi/paths/supplychain/` (package
`supplychain`).

## Ownership

This leaf owns the OpenAPI path fragments for the supply-chain routes:
container image inventory, vulnerability impact, security alerts,
advisories, SBOM attestations, and suppression mutations — see `README.md`
for the full file-to-constant map. `openapi/spec.go` concatenates the
resulting identifiers, most of them individually and `Routes` (which
itself composes `impactFindings + impactExplain + suppressionMutation`)
alongside them.

- This package MUST NOT import `openapi` — the parent imports this
  package, and the reverse would cycle.
- `impact_findings.go` and `impact_explain.go` both reference `RuntimeContext`
  from `runtime_context.go` in this package. Do not duplicate that JSON
  if a third file in this package later needs the same shape — extend the
  shared import instead, and do not move the fragment itself into this
  package (it would recreate the cycle `openapi/schema` exists to avoid;
  see `openapi/schema/AGENTS.md`).
- `routes.go` composes three other files' constants and holds none of its
  own — the one file in this package that does not follow the
  one-constant-per-file shape the rest use. Add a new route family's
  fragment as its own file and extend that composition; do not inline a
  new fragment's body into `routes.go`.
- These are documentation fragments, not the routes themselves. A change
  to a real `supplychain` handler's method, path, or request/response
  shape must be mirrored here or `scripts/verify-openapi.sh` will report
  drift.

## Directory size

At 16 files this package is the largest leaf under `paths/` and the
closest to the 40-non-test-file-per-directory cap the `dirgate` linter
enforces (`tools/golangci-lint-dirgate/`). Before adding another fragment
file here, run a file count and, if it is getting close to the cap,
consider whether a further split (e.g. by supply-chain subject) is due
before it becomes mandatory.

## Verification

Keep `scripts/verify-openapi.sh` green after any change (see
`openapi/AGENTS.md` for what it checks), and update
`docs/public/reference/http-api.md` in the same PR for any wire-visible
change. Run `cd go && go build ./internal/query/...` and
`gofmt -l internal/query/openapi/paths/supplychain` after any edit here.
