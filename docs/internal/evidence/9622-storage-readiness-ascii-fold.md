# Storage and readiness package-identity folds use ASCII letters only (#9622)

Follow-up to #9619 (merged as #9584-consumption-key-ascii-fold scope): the
consumption keys, suppression anchors, and readiness derived source-state
fold ASCII-only, but the remaining ecosystem, distro, and package-manager
spellings in the readiness SQL and the storage readers still followed the
database locale. On an `en_US.UTF-8` database a spelling such as `PİP`
lowered to an ASCII `pip` while the Go side kept the bytes apart.

## The change

Every SQL fold of an ecosystem, distro, package-manager, vendor-source, or
repository-class spelling now uses `LOWER(x COLLATE "C") COLLATE "default"`,
which folds ASCII letters only and so equals `packageidentity.LowerASCII`,
the fold that wrote the rows being joined:

- readiness queries: `readiness_postgres_query_targets.go`,
  `readiness_postgres_query_source.go`, `readiness_postgres_query.go`,
  `findings_queries.go`, `findings_split_queries.go`,
  `aggregates_queries.go`;
- advisory catalog: `catalog_sql.go` (projections and the `$2` filter);
- storage readers: `installed_advisory_targets.go`,
  `owned_package_targets.go`, `stored_advisory_names.go`,
  `status_registry.go`.

The Go fallbacks that feed those readers fold with `LowerASCII`:
installed-advisory normalizer and PURL-type fallback, owned-package alias
mapping, security-alert reconciliation fallback, vulnerability-intelligence
config, Artifactory package-type parser, parity support set.

Left alone on purpose (not identity): language labels, JVM code
identifiers, status/severity/source labels, permission and provenance
flags, repository names, prefix-search LIKE patterns, and the shared
schedule string-set over closed operator vocabularies (ASCII-identical,
fail-closed on lookalikes).

## Proof

Guard tests pin every converted site and fail on a planted locale LOWER
(`TestReadinessEcosystemFoldsASCIIOnly`,
`TestStoragePackageIdentityFoldsASCIIOnly`,
`TestListCatalogQueryEcosystemFoldsASCIIOnly`); lookalike tests pin that
`PİP`, `Serİlog`, and `GENERİC` keep their bytes apart from the ASCII
ecosystem. Each test fails on the pre-change tree and passes after.

## Performance Evidence:

No-regression proof on PostgreSQL 18.6 (Debian 18.6-1.pgdg12+2) with an
`en_US.UTF-8` database, measured 2026-11-03 on an isolated container.
Input shape: 200,003-row table (200,000 `npm`, 3 `alpine`, 1 `PİP`
lookalike) with a default-collation btree on the ecosystem column,
`VACUUM ANALYZE`d.

- Fold equality: `LOWER(TRIM('PİP') COLLATE "C") COLLATE "default"` =
  `pİp` (the Go `LowerASCII` result), `Serİlog` -> `serİlog`, ` NPM ` ->
  `npm`; the lookalike stays apart from `pip` while locale `LOWER` maps it
  to `pip`.
- ASCII differential: 13 ASCII spellings (mixed-case ecosystem and
  package-manager names with padding) fold byte-identically under the old
  and new forms, 0 differ.
- Index seek kept: `ecosystem = (LOWER(TRIM('Alpine') COLLATE "C")
  COLLATE "default")` plans `Index Scan using t_eco_idx`; the same probe
  with a bare `COLLATE "C"` value plans `Seq Scan` (the #9584 trap the
  trailing `COLLATE "default"` avoids).

The change alters no join cardinality for ASCII rows (differential above)
and no index eligibility (seek probe above), so no benchmark delta is
claimed and none is needed.

## No-Observability-Change:

The change adds no metric, span, log line, status, or dashboard. It folds
the same spellings with the same row sets for ASCII input; operator
surfaces are unchanged.
