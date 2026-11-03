# AGENTS.md - internal/collector/awscloud/satellite guidance

## Read First

1. `README.md` - package purpose, ownership boundary, and invariants.
2. `manifest.go` - typed satellite manifest and required-grant constructors.
3. `go/internal/component/manifest.go` - component manifest contract.
4. `go/internal/facts/cloud/aws.go` and the posture family files - emitted
   kind and schema-version sources of truth.
5. `docs/public/reference/component-package-manager.md` - user-facing CLI
   behavior.

## Invariants

- This package owns satellite identity, emitted kinds, reducer phases, and
  the required grant set. It never scans, emits, claims, or writes graph truth.
- The manifest must declare every kind the satellite can emit, including
  kinds the SDK fixture pack cannot prove yet. Never drop a kind to make
  conformance green; a missing declaration denies real output at admission.
- `PayloadSchemaRef` follows fixture-pack coverage: a ref the pack does not
  ship fails component validation.
- Reducer phases derive from the fact registry. Do not hand-edit the phase
  list; change the registry instead.
- The image ref is publish-owned. This port checks its digest-pinned shape
  and never mints, pins, or updates a digest.
- Never mint production grants from `RequiredGrants` output: lifetimes come
  from the `eshu component grant` activation flow.

## Common Changes

- Add a satellite-emitted kind by extending the family sources in
  `go/internal/facts/cloud`, then this port picks it up; add manifest and
  grant tests in the same change.
- When the SDK fixture pack ships a missing schema, the conformance
  characterization test in `go/internal/extensionconformance` must be
  rewritten to expect `StatusPassed`.

## Anti-patterns specific to this package

- Do not declare fewer kinds than the satellite emits.
- Do not set a payload schema ref the fixture pack does not ship.
- Do not import service scanners, runtime adapters, or credential handling.
