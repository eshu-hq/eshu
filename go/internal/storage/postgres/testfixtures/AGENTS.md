# AGENTS.md — storage/postgres/testfixtures

Scoped instructions for this leaf. The root `AGENTS.md` and the
`storage/postgres` `AGENTS.md` still apply; these add to them.

## Import rule

Test-only leaf: `_test.go` files import it, production code never
does. It may import `facts`, `scope`, and `testutil/postgresproof`.
It must **never** import `postgres` or `postgres/activation`: the
root package's internal test files import this leaf, so a parent
import is an import cycle (`commitActivationRepository`,
`claimActivationProjectorWork`, and `newLiveActivationRunner` stay
duplicated per test package for exactly this reason).

## What must stay conservative

- Merge bar is byte-identical twins, or twins differing only in
  package qualification (internal vs external test package).
  Diff with `diff` before merging; a semantic difference keeps both
  copies with the difference recorded in the README.
- A helper used by exactly one test package belongs in that
  package's own test files, not here. Hoist only on the second
  consumer.
- Do not migrate the `ApplyBootstrap` helpers out of their owning
  packages here; each schema owner keeps its own bootstrap helper.

## Gates that will fire on your change

- **`verify-package-docs.sh`** — this directory must keep `doc.go`,
  `README.md` and `AGENTS.md`.
- **`verify-telemetry-coverage.sh`** — test-only files carry no
  instrument; if the gate names a file here, it needs a row in
  `docs/public/observability/telemetry-coverage.md` with a
  `No-Observability-Change` marker.

## Do not

- Do not add production helpers here to share code between
  `postgres` and `postgres/activation`. Shared production contracts
  belong in `postgres/db` or another production seam, never in a
  test package.
- Do not import this package from non-test code; the `postgres`
  package importing its own test leaf would be an import cycle.
