# AGENTS.md - internal/packageidentity guidance

## Read First

1. `README.md` - package purpose, ownership boundary, and invariants.
2. `identity.go` - canonical package identity normalization rules.
3. `go/internal/collector/packageregistry/README.md` - collector fact emission
   consumer.
4. `go/internal/reducer/package_consumption_correlation.go` - reducer-owned
   package consumption matching consumer.

## Invariants

- This package owns pure identity normalization only. It must not fetch package
  metadata, commit facts, write graph state, or infer ownership.
- Preserve raw source fields alongside normalized fields so operators can debug
  why a source record joined or did not join.
- Keep ecosystem rules explicit. Do not route all package managers through one
  lowercase string rule.
- Package IDs identify package identity, not installed versions. Versioned
  identity belongs in PURL, BOMRef, or the caller's version-specific fact ID.
- Unknown ecosystem aliases must fail closed instead of guessing.

## Common Changes

- Add an ecosystem by extending `Ecosystem`, alias normalization, PURL type
  mapping, `normalizeName`, and table tests in `identity_test.go`.
- Keep new fields additive for durable fact consumers.
- Update package-registry, vulnerability, SBOM, or reducer docs when a new
  identity field becomes part of a public fact or API contract.
