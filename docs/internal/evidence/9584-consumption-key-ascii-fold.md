# Consumption keys fold ASCII letters only (#9584)

Package identity lowercased names with Go's `strings.ToLower`. Unicode case
mapping lowers U+0130 (capital I with dot above) to `i` and U+212A (Kelvin
sign) to `k`, so `ConsumptionKeys("nuget", "Serİlog")` returned the key
`serilog`. A manifest dependency spelled with a lookalike letter joined the
advisory written for the real package. That is a false finding at best, and at
worst a way for a lookalike name to inherit another package's advisory.

## The rule

In the packages listed below, the folds that turn a package name,
namespace, registry host, package manager, or ecosystem alias into identity
now use `packageidentity.LowerASCII`, which maps A-Z to a-z and keeps every
other byte. `packageidentity`, `reducer/packages/correlation`, and
`query/supply/chain/impact` were swept in the first round; the other five
packages by the census below. "Folds this change leaves alone" names the
folds that census and the reviews found. Packages outside the sweep were not
audited; see #9622. The sites:

- `go/internal/packageidentity`: `ConsumptionNameCandidates`,
  `ManifestEntityNameCandidates`, `normalizeName` for every ecosystem (so
  `Normalize`, `PackageID`, and PURLs too), `NormalizeEcosystem`,
  `NormalizeRegistry`, PURL type parsing, and the package-manager fold.
- `go/internal/reducer/packages/correlation`: `SecurityAlertPackageNameMatches`,
  the RubyGems Gemfile-to-lockfile join key, and the `package_manager` and
  registry `ecosystem` folds that feed the in-memory key builder.
- `go/internal/collector/vulnerabilityintelligence`: the OSV Swift
  reference parser and the OSV ecosystem fallback, so the advisory side folds
  like the manifest side; in `vulnruntime`, the derived scope-id, slot, and
  configured ecosystem tokens and the OSV query ecosystem; in `checkpoint`,
  the source-state ecosystem.
- `go/internal/reducer/supplychain/core`: vulnerability suppression scope.
  `package_id` and `purl` anchors compare with `LowerASCII` on both sides.
  `strings.EqualFold` treats U+212A as `k` and U+017F (long s) as `s`, so a
  suppression written for `npm://registry.npmjs.org/kafka` hid findings on
  the Kelvin-sign lookalike. CVE and advisory IDs keep `EqualFold`: they are
  ASCII by specification. The sbt package-manager checks fold ASCII only too.
  Go reachability folds module paths with `LowerASCII` where it groups module
  evidence and advisories and where it looks them up, so a lookalike module
  no longer merges with the real one. The ecosystem, package-manager, distro,
  vendor-source, OS package, npm name, and PyPI PURL-type folds in this
  package use it too.
- `go/internal/collector/scannerworker/sbomgenerator` and
  `go/internal/collector/sbom/document`: SBOM component identity (generated
  SBOMs) and the CycloneDX and SPDX duplicate key. With a Unicode fold, a
  component listed as `pkg:npm/Kafka` (Kelvin sign) ahead of the real
  `pkg:npm/kafka` took its identity, and the generator dropped the real
  component as a duplicate.
- `go/internal/coordinator/vulnerability`: the derived OSV scope-id writer
  (slot, package, and batch scope ids) and the batch scope sizer, so the
  writer folds the ecosystem segment the way `vulnruntime` parses it; the
  SBOM PURL-type ecosystem; and the sbt owned-target check.
- `go/internal/query/supply/chain/impact`: the derived OSV source-state
  lookup in readiness folds package names and ecosystem spellings with
  `LOWER(TRIM(x) COLLATE "C") COLLATE "default"`. A plain `LOWER` follows
  the database collation; on libc, musl, ICU, and `pg_c_utf8` it lowers
  `Serİlog` to `serilog`, while the state rows were keyed by `LowerASCII`.

No registry Eshu reads defines a Unicode case fold for package names. PyPI
names, Go module paths, and crates.io names are ASCII-only; npm rejects names
that are not URL-safe; NuGet IDs may not hold characters that are not valid in
a URL. The citations are in `go/internal/packageidentity/README.md`. A
non-ASCII name keeps its non-ASCII letters in its keys and joins only an
identically spelled record. One residual is left open: nuget.org validates new IDs with an ASCII-only
pattern but still reads legacy IDs with `\w`, and it compares them with .NET
`ToLowerInvariant`. Two legacy IDs that differ only in non-ASCII case are one
package there and two keys here. Not joining them is the fail-closed
direction; #9594 tracks confirming it.

### Folds this change leaves alone

`rg -n 'strings\.(ToLower|EqualFold)' --glob '!*_test.go'` over the swept
packages (`supplychain/core`, `vulnerabilityintelligence`, `sbomgenerator`,
`sbom/document`, `coordinator/vulnerability`) lists what remains:

- Version strings, range kinds (`SEMVER`, `ECOSYSTEM`), CVE and advisory
  IDs, CVSS vectors, severity and state labels, language tags,
  missing-evidence text, advisory feed labels, scope anchors (repository,
  service, workload, digest), CPE criteria, secret-key redaction, hashes
  and subject digests, SPDX and CycloneDX spec tokens, lockfile source-type
  and scope labels, and URL schemes. None of these is package identity.
- Code-identifier matching in reachability: Python import roots and SCIP
  symbols, and JVM package segments. These compare source-code identifiers,
  which Python and Java allow to be non-ASCII, and Python normalizes
  identifiers with NFKC (which maps U+212A to `K`), so NFKC is the correct
  Python fold, not the registry rule. These matches only add reachable
  evidence: on a match they set a positive label (`python_parser_call`,
  `jvm_package_api_reachable`, ...), and on no match they report missing
  evidence. They never produce a `not_called` verdict, so a loose fold can
  over-report reachability but cannot hide a finding.
- Identity folds found outside the swept packages, tracked in #9622 (not an
  exhaustive list; those packages were not audited):
  `go/internal/storage/postgres/installed_advisory_targets.go` (the Go
  ecosystem, distro, package-manager, vendor-source, and repository-class
  folds, and the SQL `LOWER`s on the same columns),
  `go/internal/storage/postgres/owned_package_targets.go` (the SQL
  `LOWER(package_manager)` expression and the Go ecosystem fold),
  `go/internal/reducer/securityalert/reconciliation_triage.go` (the ecosystem
  fallback fold), and the readiness SQL ecosystem `LOWER`s outside the derived
  source-state lookup (targets and source CTEs). These read collector- and
  parser-authored values; converting the SQL needs live plan proof.

## Proof

- RED before the fix: `TestConsumptionKeysKeepLookalikesApart` failed with
  `manifest name "Serİlog" joins "SerIlog" via key {Ecosystem:nuget
  PackageName:serilog}`, and the same for the Kelvin sign in every ecosystem.
  `TestRegistryIdentityKeysKeepLookalikesApart` failed because `Serİlog` and
  `SerIlog` shared package ID `nuget://api.nuget.org/v3/index.json/serilog`.
  The reducer tests (`SecurityAlertPackageNameMatches`, the RubyGems range
  join) and the OSV Swift parser test failed on the old code the same way.
- ASCII keys are byte-identical: the code before the change wrote
  `go/internal/packageidentity/testdata/ascii_keys_golden.json` over a corpus
  of 56 names across every ecosystem and alias (PyPI separator variants, Maven
  `group:artifact` and `group/artifact`, npm scopes, Go module paths with
  uppercase, Composer, Hex organizations, Swift, Pub, OS), plus registry
  identities, PURLs, and package IDs. `TestASCIIKeysMatchPre9584Golden`
  passes unchanged after the fix.
- Parity: `TestManifestConsumptionKeysMatchReducerBuilderForMixedCorpus`
  feeds one mixed ASCII and lookalike corpus through the sidecar writer and the
  reducer's in-memory builder and requires the same key set per fact. It
  failed first on a real divergence: the reducer lowercased `package_manager`
  with `strings.ToLower`, so a `pypİ` row got keys in the reducer and none in
  the sidecar. Both sides now agree.

## Stored keys

`package_manifest_consumption_keys` and `package_registry_identity_keys` rows
written before this change hold the old fold, for non-ASCII names only. ASCII
rows are unchanged (golden proof above).

- The reducer does not read stale keys. It reads manifest rows by
  `entity_name` and recomputes keys from the payload with
  `ManifestConsumptionKeys` as the accuracy gate, so no finding comes from a
  stale key.
- The readiness package-anchored arm does
  (`readiness_package_consumption_query.go`, `$20`). It joins target keys to
  the sidecar by equality.
- Only U+0130 and U+212A lower to an ASCII letter under `strings.ToLower`
  (exhaustive rune scan in the review). So a stale key can count toward an
  ASCII package's `package.consumption` readiness only for a fact whose name
  holds one of those two runes, until the scope's next generation rewrites
  its keys. Other stale non-ASCII keys (`É` folded to `é`) match only
  non-ASCII targets. This is a status surface, not a finding.
- `package_id` values inside already-stored registry and correlation fact
  payloads for those names keep the old conflated ID until the collector
  re-emits them. They heal on the next collection.

The arbiter ruled option C: stored keys stay as written, with no repair
(#9593 is closed as not needed). The QA environment had no residue to
repair. A read-only census found 3,437,025 manifest and registry facts
across 595 scopes with no package name holding U+0130 or U+212A, and no
non-ASCII name at all. The sidecar count (0 non-ASCII `package_name` rows
of 3,440,490) agrees, but it cannot show the risky class on its own: the old
fold stored a U+0130 or U+212A name under an ASCII key.

New writes are correct: the live readiness test
(`TestSupplyChainImpactReadinessPackageConsumptionScopeLive`, case "package
excludes a non-ASCII lookalike consumer") seeds a `kafka` scope and a
Kelvin-sign `Kafka` scope, writes the sidecar through the production
backfill writer, and counts 1 kafka consumer. With `LowerASCII` forced back
to `strings.ToLower` the same test counts 2.

## Performance

Benchmark Evidence: `LowerASCII` replaces `strings.ToLower` on the write path
of every manifest dependency fact. Base `2fad8ecdab` and the change were built
as two test binaries and run in alternating rounds, then compared with
`benchstat`. Allocations and bytes are identical: the 56-name corpus is 699
allocs/op on both sides, and the `ManifestConsumptionKeys` cases are 11, 11,
16, and 27 allocs/op on both sides. There is no significant time change (the
review's interleaved rerun: corpus 46.90 µs vs 47.06 µs, p=0.393). Both runs
shared a loaded host, so they claim no regression, not a speedup.

The readiness SQL fold keeps its index seek. On PostgreSQL 18.6 with an
`en_US.UTF-8` database, a lateral probe of a `(ecosystem, package_name)`
btree with a `LOWER(TRIM(x) COLLATE "C")` value planned a Seq Scan, because
the `C`-collated value cannot use the default-collation index. With the
trailing `COLLATE "default"` the plan is
`Index Scan using st_ecosystem_package_name_idx`, and the value is still
the ASCII-only fold (`serİlog`, `Kafka` with the Kelvin sign, `École`).

## Observability

Observability Evidence: `eshu_dp_package_manifest_non_ascii_names_total`
(counter, closed `ecosystem` label) counts the manifest facts the ingester keys
under a non-ASCII name. It is emitted from the ingestion fact-batch callback in
`go/internal/storage/postgres/ingestion.go`. Each increment comes with an
`event_name=package_manifest_consumption_keys.non_ascii_names` log line that
carries `scope_id`, `generation_id`, `ecosystem`, and `rows`.
`TestRecordPackageManifestNonASCIINamesEmitsCounterByEcosystem` reads the
counter back through an OTel manual reader, and
`TestCommitScopeGenerationCountsNonASCIIManifestNames` reads it after a real
`IngestionStore.CommitScopeGeneration`. The counter is added inside the
ingestion transaction, so it counts per commit attempt.
