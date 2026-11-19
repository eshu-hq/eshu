# Arbiter ruling: 9669 cutover step 2 (core read service) scope (2026-11-18)

Recorded by the coordinator; the arbiter does not edit files. Arbiter:
`arbiter-eshu`. Parent: `eshu-hq/eshu#9669`. Cutover record:
`eshu-hq/eshu-api#101`.

Heads verified: core `88f1fd2a22` (origin/main), satellite `e0a6eb8`
(post-v6; step 2 builds against satellite origin/main `08ebde4`, which
contains the same `api/` + `contracts/` surface — the v0.6.0 delta only
touches `release.yml`, docs, and the workflow pin test).

Sources read: satellite `contracts/upstream/api_boundary.go`,
`api/dispatch.go`, `api/w8a_wave.go` + W8a suite
(`w8a_conformance/auth/failclosed/parity_get/parity_post/latency/openapi`
tests), `cmd/eshu-api/main.go` + readyz tests, `docs/parity.md` census,
core `go/internal/query/g2_read_admission_wire_test.go`,
`go/internal/query/ports.go`, D13 docs. Per-family `*-read-admission.md`
docs sampled (status-governance, status-hosted-readiness, supply-chain,
compare, provider-config, code-flow); the pattern is uniform.

## Binding decisions

1. **Scope is the forwarded method set, not a route count.** Step 2
   implements every read method the satellite still forwards
   (whole-read-surface-ward: the 228-read census from `parity.md` minus
   already-terminated D-lane/W8a rows, as enumerated at a pinned
   satellite commit) **plus the 19 core-owned survivor mutations** named
   by the goal. The numbers "26 routes" and any stale census count are
   **not** the scope control — wave landings change route counts, frozen
   method sets do not. The scope artifact must be an explicit frozen
   list (method+path+family per row) pinned at the satellite commit
   step 2 builds against; a row not on that list is out, a row on it
   is in.
2. **Contract hierarchy when sources disagree (highest wins):**
   (a) versioned admission interfaces + frozen method-set tests (they
   fail the build on drift — e.g. `TestCoreReadContractsVersioned`,
   `TestReadStoreContractIsVersioned`, per-family
   `...ContractIsVersioned`); (b) per-family `*-read-admission.md` pin
   docs under `CoreReadContractsVersion v0`; (c) `api_boundary.go`
   umbrella version as the cross-repo move signal, not a shape
   definition; (d) `contracts/upstream/*-read-wire-shapes.txt` parity
   stubs last — they are byte-shape **snapshots**, regenerated on
   deliberate change, never edited to bless a drift. A stub disagreeing
   with a pin means the stub (or the code) is wrong, never the pin.
3. **G2 pins vs parity stubs.** The G2 read-admission pins
   (extraction-readiness, fact-schema, surface-inventory pattern in core
   `g2_read_admission_wire_test.go`, "G2-five" per the D13 seam doc) are
   enforcement: handler legs must admit **through the injected pin**,
   with hermetic canned-pin tests proving it. Parity stubs prove wire
   bytes. If a stub passes while the pin test fails, the lane is red —
   pins govern admission, stubs govern shape.
4. **Oracle discipline for deleted `go/cmd/api` handlers.** Observable
   contract may be carried over: route path, HTTP method, status codes,
   envelope/error-code shapes (all already pinned by satellite parity
   tests). Everything else must be re-derived, never ported: handler
   internals (no copy-paste — new code, new pins), authz logic (core
   re-judges every call; satellite's shared-key gate is not the
   authorization), and error synthesis (the only locally synthesized
   error is the permanent `backend_unavailable` 503 with no
   `Retry-After`; all core-answered bytes cross untouched). A step-2
   diff containing moved handler bodies verbatim fails review regardless
   of test color.
5. **`--core-base-url` / `/readyz` semantics for the new service.**
   Satellite semantics are fixed and the core service mirrors them:
   `--core-base-url` is required and the process refuses to start
   without it; `/readyz` stays public and unauthenticated, 200 against
   a live core, 503 with a dead/unconfigured core. For the **new core
   service**, "live" means its real backends (Postgres + pinned Neo4j)
   are reachable — a process-liveness-only 200 is non-compliant.
   `/health` stays a shallow process check; `/readyz` is the
   backend-gated verdict.
6. **Port placement: `go/internal/query`, no new tree.** `ports.go`
   already aliases `GraphQuery`/`ContentStore`; family
   `*ReadAdmission` interfaces live beside their handlers in
   `go/internal/query/*.go`. Per-method ports go there (new per-family
   files as needed, honoring the 500-line file cap). A new top-level
   tree is rejected: it breaks the ownership boundary
   (`sync → discover → parse → emit → reducer → query surface`) and the
   existing admission-file convention for zero benefit.
7. **Per-method proof is the W8a template, all seven legs, no partial
   credit.** Every method ships: re-declared port + frozen-set test +
   conformance + auth + fail-closed + parity GET/POST + latency +
   OpenAPI spec test, plus the `contracts/` termination record with
   rollback (revert-to-relay, per the `w8a_terminate.go` lane rule).
   The W8a disclosed-absence posture stands: a natively mounted route
   whose fragment stays core-side must disclose it in the termination
   record and the spec-key list, not silently drop the key.
8. **Done-criteria restatement (measurable form).** "Satellite
   `make ci` green" = satellite CI at the pinned commit with the new
   core as `--core-base-url`. "Core floor green" = `make pre-push` on
   the step-2 worktree. "Every row suite green" = each frozen-list
   row's seven-leg suite green, enumerated by name. "Serves `/readyz`
   200 against live backends" = `curl /readyz` → 200 with live
   Postgres/Neo4j, 503 with either down — both states demonstrated,
   not just the happy path. Anything in the current wording resembling
   "whole surface works" without the frozen list, or a latency claim
   without naming the bound (satellite `dispatchTimeout` 30s /
   `readclient.DefaultTimeout`, pinned equal by test), is unmeasurable
   and does not count as done.

## Explicit non-goals

- No new auth model (D8 owns per-class resolution; shared-key
  terminate + explicit relay + dest-401 standing rules carry over
  unchanged).
- No mutation semantics beyond the 19 named survivors;
  suppressions-style `Never-satellite` rows stay core-side via their
  existing admissions.
- No OpenAPI fragment reshaping: byte-identical ports or disclosed
  absence only (`scripts/check-parity.sh` stays green).
- No telemetry expansion beyond what a runtime-affecting change already
  requires; the D13 seam lane's no-observability-change precedent
  applies where no new path executes.
- No performance claim without before/after on the touched path;
  struct-literal-only wiring takes the no-regression-evidence form, not
  a manufactured speedup.
