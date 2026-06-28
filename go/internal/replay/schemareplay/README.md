# replay/schemareplay

Schema-version compatibility replay for the deterministic replay framework
(epic #4102, issue #4127, R-18). It proves that **old-schema cassettes replayed
against current code are never silently wrong** — each historical-version fact
is either admitted or cleanly refused.

## What it proves

`ReplayAdmission` loads a frozen cassette and drives every recorded fact through
the production admission function `facts.ValidateSchemaVersion` (the same
per-fact `AdmissionHook` the projector wires — `projector/schema_version_admission.go`
delegates to it). Each fact's outcome is pinned in `schemareplay_test.go`:

| frozen fact | version | outcome |
|---|---|---|
| `aws_resource` | `1.0.0` (current) | **admit** |
| `aws_resource` | `0.9.0` (older major) | **refuse** — `unsupported` |
| `replay.unknown_legacy_kind` | `1.0.0` | **admit** — core owns no versioned schema (pass-through) |
| `aws_resource` | `1.2.0` (newer than known) | **refuse** — `unsupported` |
| `aws_resource` | `legacy-2019` (pre-semver) | **refuse** — fails closed |

The admit/refuse asymmetry is the teeth: a no-op admission would fail the three
refusal pins.

## The version-bump guard

`TestSchemaVersionRegistryPinForcesCompatibilityCase` pins each corpus kind's
supported `schema_version` against the central registry (#3152). If a
contributor bumps a fact's version, the guard fails with instructions to either:

- add a frozen replay case proving the **older** version still admits (a proven
  migration path), or
- add an **explicit asserted refusal**

…in the same change. This is the issue's second acceptance bullet: a new
`fact_schema_version` cannot land without a corresponding replay decision.

## Why a real core kind

The corpus uses `aws_resource` (a registered, core-owned versioned fact kind at
`1.0.0`) so the admission decisions exercise the real registry, not a synthetic
stand-in. All payload data is synthetic — no real ARNs, account IDs, or hosts.

## Verifying a change

```bash
export GOCACHE="$(git rev-parse --show-toplevel)/.gocache"
cd go && go test ./internal/replay/schemareplay/ -count=1
```

No Docker, no Postgres, no graph backend — runs in the default `go test` pass.
