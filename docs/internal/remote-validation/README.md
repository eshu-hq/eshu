# docs/internal/remote-validation

Evidence artifacts for `remote_validation` proof-IDs cited in
`specs/capability-matrix.v1.yaml` and `specs/capability-matrix/*.yaml`
(#5407, PR 2 of #5336).

## Convention

A matrix row's `production` profile may cite a `remote_validation` proof, for
example:

```yaml
production: {status: supported, verification: [{remote_validation: prod-code-search-exact}]}
```

That `prod-code-search-exact` ref must resolve to a committed file at:

```
docs/internal/remote-validation/prod-code-search-exact.md
```

`go/internal/capabilitycatalog/remote_validation.go`
(`CheckRemoteValidationArtifacts`) enforces this with `os.Stat`, run by
`scripts/verify-remote-validation-artifacts.sh` (CI gate
`remote-validation-artifacts` in `specs/ci-gates.v1.yaml`). A ref that
resolves to no file here fails the gate unless it is listed in the burn-down
baseline, `specs/remote-validation-baseline.txt`.

A ref is validated against the slug shape `^[a-z0-9]+(-[a-z0-9]+)*$` **before**
it is ever joined into a path or probed with `os.Stat`, and the resolved path is
re-checked to stay under this directory. A ref that is not a valid slug (for
example a `../../etc/passwd` path-traversal payload) can neither escape this
directory nor be excused by a baseline entry: it is always a hard finding naming
the ref and its capability/profile.

The baseline is a **frozen audited set**, not a soft "known debt" list. Every
slug in it is a capability-matrix row whose `production` profile claims
`{status: supported}` and whose sole verification evidence is a
`remote_validation` ref that resolves to no committed artifact — this directory
did not exist when the gate was introduced, so each is a top-tier
production-support claim resting on zero committed proof. Freezing the set stops
the debt from growing; it does not cure the claims. The systemic per-row
validate-or-downgrade of every baselined slug is tracked in **#5552, which
blocks epic #5344 closure**.

The baseline carries a `# FROZEN_MAX: <N>` directive that acts as a ratcheting
high-water mark. The gate fails when the entry count **exceeds** the ceiling, so
a new unverified `production:supported` row cannot be smuggled in by appending
its ref and running `-update`. Burning down a slug and running `-update` lowers
`FROZEN_MAX` to the new, smaller count; `-update` never raises it. Raising the
ceiling requires an explicit, separately-reviewed one-line edit.

The `FROZEN_MAX` ceiling alone does **not** stop a constant-count *atomic swap*:
in a single edit an attacker could burn down one legitimately baselined ref
(committing its artifact, so it leaves the baseline) while adding a new unbacked
claim, keeping the entry count at the ceiling. To close that, the baseline is
bounded by an **immutable frozen membership set**,
`specs/remote-validation-frozen.txt` — the audited-at-introduction set of the
115 slugs. The gate enforces `baseline ⊆ frozen`: a ref may be baselined only if
it is also in the frozen set, so a new claim (absent from the frozen set) is
rejected even at constant count. The frozen file loads **fail-closed** (a
missing or malformed file fails the gate). `-update` **never** writes the frozen
set; it only regenerates the baseline (the dangling subset of the frozen slugs).
A slug leaves the frozen set only when its row is validated-or-downgraded and
removed from **both** files in the same reviewed edit.

## Writing an artifact

An evidence file should record what was actually run against a real
deployed-services environment: the command or workflow, the environment
(sanitized — no credentials, hostnames, or account IDs), the date, and the
observed pass/fail outcome. It does not need to be a specific format; it
needs to be enough for a reviewer to judge whether the claim it backs is
real. Once the file exists, remove the ref from
`specs/remote-validation-baseline.txt` (or run
`bash scripts/verify-remote-validation-artifacts.sh -update`, which drops it
automatically because `remoteValidationArtifactExists` now returns true and
also ratchets `FROZEN_MAX` down to the new, smaller count). Removing the ref
from `specs/remote-validation-frozen.txt` as well is optional but keeps the two
files aligned; `-update` does not touch the frozen file.

## Current state

This directory is empty as of #5407: every `remote_validation` ref cited in
the matrix at freeze time (115 unique slugs across 120 row-occurrences) predates
this gate and had no committed evidence file. All 115 were frozen in
`specs/remote-validation-frozen.txt` (the immutable audited-at-introduction set)
and, at the time, also carried in `specs/remote-validation-baseline.txt` under
`FROZEN_MAX: 115`. Closing an entry requires either committing a real artifact
or an explicit, separately-reviewed decision to downgrade the capability's
claimed status; the frozen set only shrinks when a slug is validated-or-
downgraded and removed from both files in the same reviewed edit. The
systemic burn-down of all 115 is tracked in #5552, which blocks epic #5344
closure.

TRANCHE 1 (#5552) closed the pair #5336 originally flagged,
`prod-component-extension-inventory` and `prod-component-extension-diagnostics`
(`component_extensions.inventory` / `component_extensions.diagnostics`), by
**downgrading** both capabilities' `production` profile from
`supported` to `experimental` rather than committing a deployed-registry
evidence artifact. See [DISPOSITIONS.md](DISPOSITIONS.md) for the per-row
record and rationale. `FROZEN_MAX` is now 113, and both slugs are removed from
`specs/remote-validation-baseline.txt` and `specs/remote-validation-frozen.txt`.
