# AGENTS.md - cli/support

## Read first

1. `doc.go` for the package contract.
2. `README.md` for ownership and invariants.
3. `docs/internal/evidence/8874-s4-endpoint-decisions.md` for the
   endpoint-vs-retain rationale behind each route.
4. Parent `../AGENTS.md` for query-wide invariants (envelope negotiation,
   error codes) that the handlers reuse without redefining.

## Invariants

- Handlers stay pure functions of the request body. Adding a backend read
  (graph, Postgres, collector, queue) breaks the package contract and the
  S4 decision record — that belongs in a capability route, not here.
- Never import this package from `query`, `serviceintel`, `reportbundle`,
  or `answerquality`. Mount the routes from `go/cmd/api` wiring only.
- Preserve response status contracts: 200 verdict-or-bytes, 400 malformed,
  413 oversize, 422 capture refusal, 500 internal. A new status needs the
  OpenAPI fragment, the handler test, and the http-api doc updated together.
- Add tests before changing handler behavior (TDD). Contract tests must
  keep the differential proof: endpoint output byte-identical to the local
  function for the same input, with CreatedAt/BundleID normalized on the
  capture path (distinct ids by design).
- Keep the OpenAPI fragments, `openapi/spec.go` concatenation, the surface
  inventory (regenerate via `capability-inventory`), and the http-api doc
  in lockstep with every route change.

## Verification

```bash
(cd go && go test ./internal/cli/support ./internal/reportbundle ./internal/serviceintel ./internal/answerquality -count=1)
bash scripts/verify-openapi.sh
scripts/verify-package-docs.sh
```
