# AGENTS.md — cmd/core-api guidance for LLM assistants

## Read first

1. `go/cmd/core-api/README.md` — binary purpose, flags, configuration, and
   probes
2. `go/cmd/core-api/main.go` — `run` and `serve`; flags, store wiring,
   extras mounting, and `ListenAndServe` live here
3. `go/cmd/core-api/main_wiring.go` — `buildCoreAuthWiring`,
   `openCoreStores`, OIDC bearer resolver, v1 alias and deprecation
   middleware
4. `go/internal/query/serve.go` — `NewCoreServeMux`, `BuildCoreRouter
   deps, probes, and the auth chain; handler wiring lives there

## Invariants this package enforces

- **Thin main** — `main.go` does flag parsing, store wiring, and
  `ListenAndServe` only. Route registration, the auth middleware chain,
  and handler wiring live in `go/internal/query`. A domain-logic change
  here is a violation.
- **No `--core-base-url` flag, no forwarding client** — the binary never
  dials any API backend (one-way no-loop invariant, step-3 ruling D3). A
  flag or HTTP client that points at another API origin is a violation.
- **Secret boundary** — adapters that touch sealed secrets
  (`adapters_*.go`, `seed*.go`) live in this package because
  `go/internal/query` must never import secretcrypto
  (`TestNoQueryFileImportsSecretcrypto`). Moving one into query fails the
  build loudly; keep them here.
- **Pinned Neo4j only** — `serve` fails startup unless
  `ESHU_GRAPH_BACKEND=neo4j`. Do not add backend fallbacks here; the
  deployment pins the backend.
- **Fail-closed wiring** — a missing database yields fail-closed handlers
  (503 at request time), never a panic and never an unmounted route. A nil
  handler in `CoreAuthWiring` mounts the empty fail-closed shape.

## Common changes and how to scope them

- **Add a flag** → add it to the `flag.FlagSet` in `run`; only the listen
  address is a flag today — operator input arrives through env. Why: the
  ruling fixes the command/args surface to core-owned inputs.
- **Add an auth-family handler** → build it in `buildCoreAuthWiring` and add
  the field to `query.CoreAuthWiring`; mounting stays in
  `mountCoreAuthHandlers`. Why: mounting without wiring leaves a 503-only
  route, wiring without mounting leaves an unserved frozen row — the frozen
  test catches both.
- **Change `/readyz` semantics** → edit `checkCoreReadiness` in
  `go/internal/query/serve.go`, not here. Why: the probe is core-owned and
  shared with the frozen served-method list (`GET /readyz`).

## Failure modes and how to debug

- Symptom: binary exits with `requires the pinned Neo4j backend` → cause:
  `ESHU_GRAPH_BACKEND` is unset or not `neo4j` → set it to `neo4j`.
- Symptom: `/readyz` is 503 with live backends → cause: Postgres ping or
  Neo4j `VerifyConnectivity` fails under the 2 s probe bound → check DSNs
  and that both backends accept connections from this host.
- Symptom: every `/api/*` route is 401 → cause: `ESHU_API_KEY` (or another
  auth source) is configured but the caller sends no/wrong credential →
  check the `Authorization` header.

## What NOT to change without an ADR

- The `--addr`/`--version` flags and the no-`--core-base-url` rule.
- The secret boundary (adapters stay in package main).
- The frozen served-method list (regenerate with
  `scripts/gen-core-served-methods.sh`, never hand-edit).

## Evidence and change history

- Wave-6 lane (2026-11-19): new binary, mux constructor, frozen
  enumeration, and image row. Parent rulings:
  `docs/internal/arbiter/9669-step3-gating-ruling-2026-11-19.md`,
  `docs/internal/arbiter/9669-step2-scope-ruling-2026-11-18.md`.
