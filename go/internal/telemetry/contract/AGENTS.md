# AGENTS.md — internal/telemetry/contract guidance for LLM assistants

## Read first

1. `go/internal/telemetry/contract/README.md` — ownership and inventory
2. `go/internal/telemetry/contract/doc.go` — the godoc contract
3. `go/internal/telemetry/AGENTS.md` — the parent package's full contract
   rules (frozen keys, `eshu_dp_` prefix, no high-cardinality labels); every
   rule there applies here too

## Invariants this package enforces

- **Declarations only, no registration** — this package defines constants,
  bounded label values, and small `Attr*` helpers. It MUST NOT gain a
  `func init()` or mutate any package-level slice. Registration order lives
  in root `go/internal/telemetry/registration.go` and
  `registration_steps.go`.
- **Leaf contract** — no `go/internal/*` imports, including root package
  `telemetry` (that would cycle, since root imports this package).
- **Frozen names** — every `Span*`, `MetricDimension*`, and `LogKey*`
  constant here is part of the same frozen contract root `contract.go`
  defines. Do not rename or remove one; add a new constant instead.

## How to add a new span, dimension, or log key to an existing family

1. Add the constant to the relevant family file here (e.g. a new CI/CD span
   goes in `cicd.go`).
2. In root `go/internal/telemetry/registration.go` or
   `registration_steps.go`, find that family's `registerXxx` function and
   splice the new name into its anchor-insert or append call, referencing it
   as `contract.NewName`.
3. Add the new name to the expected list in the appropriate root
   `TestSpanNames`, `TestMetricDimensionKeys`, or `TestLogKeys` case in
   `go/internal/telemetry/contract_test.go` — these tests pin the exact
   frozen splice order and MUST be updated deliberately, never bypassed.
4. Add the root compat alias in `compat_contract.go` (or
   `compat_observability.go` / `compat_thirdparty.go` for those
   subpackages) so existing external callers can keep using the bare root
   name.
5. Run `go test ./internal/telemetry/... -count=1` from `go/`.

## How to add a new family

1. Create a new file here (or under `observability/` or `thirdparty/` for a
   hosted-observability or third-party source) following the naming and
   package rules in `docs/internal/naming.md`.
2. Add a `registerXxx` function to root `registration.go` or
   `registration_steps.go` and append it to the `registrationSteps` slice at
   the position that reproduces the intended frozen order — read that
   file's own load-bearing-order comment before touching the slice.
3. Add the compat aliases and update the three order tests as above.

## What NOT to change without discussion

- The registration order in root `registrationSteps` — it is load-bearing
  and anchor-based; reordering it silently changes `SpanNames()`,
  `MetricDimensionKeys()`, or `LogKeys()` output order for every caller.
- The `z_`/`zz_`/`zzz_`/`zzzz_` file-name prefixes on the files that carry
  them — kept for history per the owner-approved target tree, even though
  they no longer control order.
