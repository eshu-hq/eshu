# AGENTS.md - internal/packageidentity guidance

## Read First

1. `README.md` - package purpose, ownership boundary, and invariants.
2. `identity.go` - canonical package identity normalization rules.
3. `go/internal/collector/registry/package/README.md` - collector fact emission
   consumer.
4. `go/internal/reducer/packages/correlation/consumption.go` - reducer-owned
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
- Fold case with `LowerASCII`, never `strings.ToLower`, `unicode.ToLower`, or
  `strings.EqualFold`, anywhere a package name, namespace, registry, or
  ecosystem becomes identity (#9584). The manifest side and the advisory side
  must fold identically; `testdata/ascii_keys_golden.json` pins ASCII keys
  byte-for-byte and `lookalike_test.go` pins the non-ASCII rule. Code that
  compares package identity outside this package follows the same rule in Go:
  package IDs and PURLs compare with `LowerASCII(a) == LowerASCII(b)`. SQL
  that joins a package name or ecosystem to a key Go wrote folds with
  `LOWER(TRIM(x) COLLATE "C") COLLATE "default"`. The only site converted
  and pinned today is the readiness derived source-state lookup
  (`TestReadinessDerivedSourceStateFoldsASCIIOnly`). Other SQL ecosystem and
  package-manager `LOWER`s still follow the database collation: the
  readiness targets and source CTEs, `installed_advisory_targets.go`, and
  `owned_package_targets.go` (follow-up #9622). The evidence note
  `docs/internal/evidence/9584-consumption-key-ascii-fold.md` lists the
  converted packages and the folds left alone that the census of the swept
  packages and the reviews found. Packages outside the sweep were not
  audited; see #9622.
- Unknown ecosystem aliases must fail closed instead of guessing.
- `sbt` is not an ecosystem alias. A `%%` row's name is the unsuffixed
  `organization:name`, which is not a published artifact, so sbt rows resolve
  only through `ManifestConsumptionKeys` from `sbt_cross_version` and
  `sbt_binary_artifact`. Add a case there with a positive, a negative, and an
  ambiguous test; never add `sbt` to `NormalizeEcosystem`.

## Common Changes

- Add an ecosystem by extending `Ecosystem`, alias normalization, PURL type
  mapping, `normalizeName`, and table tests in `identity_test.go`.
- Keep new fields additive for durable fact consumers.
- Update package-registry, vulnerability, SBOM, or reducer docs when a new
  identity field becomes part of a public fact or API contract.
