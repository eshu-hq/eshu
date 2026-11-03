# #9929: a vendor exact version list beats an OSV single-fix shortcut

Refs #9929. Part of #9209.

## What changed

An OS advisory fact that carries release evidence (a vendor fact such as
`source: debian`, or a release-tagged fact) is read as published for the host's
release. The join does not check that: a vendor fact carries no release line, so
its list is matched by exact version string only (see Known limitation). When
the list names the host version, the host reads `affected_exact` under the existing
`dpkg_exact_affected_version` / `apk_exact_affected_version` reasons. That
answer is now read before every verdict built from untagged OSV ranges
(ambiguous, unverified, unlisted, malformed), before the unlisted settlement,
and before the single-fix shortcut. Before this change the shortcut guard
skipped every fact that carries release evidence, so an OSV single fix at or
below the host answered `not_affected_known_fixed` over a vendor list that
named the host.

The #9927 row `TestUntaggedVendorVersionListKeepsSingleFixShortcut` pinned the
old answer on purpose. This change flips it:
`TestUntaggedVendorVersionListBeatsOSVSingleFix` asserts `affected_exact`.

## Unreadable list entries

An entry the family comparator cannot read never counts as naming the host.
It is not dropped either:

- A readable entry that names the host wins wherever it sits, so the verdict
  does not depend on entry order.
- With no readable match, an unreadable entry on a fact that carries release
  evidence keeps the single-fix shortcut and the unlisted settlement from
  claiming known fixed. The scan then reports `possibly_affected` /
  `malformed_advisory_range`, the closed reason it already gave an unreadable
  list entry (fail closed).
- An unreadable entry on a fact without release evidence (untagged OSV) keeps
  the earlier rule: left to the later scan, which a shortcut can still skip.
  This change does not touch that case.

No new reason string, counter, span, or log key was added.

## Census (read-only)

Question: how many live findings move from known fixed to affected.

Answer: zero. Two independent reasons:

- **Code path.** Only two collectors emit `vulnerability.affected_package`: OSV
  (`osvSourceName`, `go/internal/collector/vulnerabilityintelligence/envelope.go`)
  and GLAD (`gitlab_gemnasium_envelope.go`). Neither emits an OS vendor source
  such as debian, ubuntu, or alpine, and the NVD envelope carries no
  `affected_versions`. GLAD facts carry language ecosystems, which an OS host
  rejects. An OSV fact whose advisory id starts with `GHSA-`, `PYSEC-`, `GO-`,
  `RUSTSEC-`, or `MAL-` has release evidence by id class, but the join keys on
  that prefix class, which never equals an installed row's distro vendor
  source, so it cannot join an OS host. A tagged fact (`ReleaseLine`) drops its
  list. So a vendor list reaches the guard only from a test fixture today.
- **Query.** Run on the QA environment's read replica
  (`pg_is_in_recovery() = t`, `default_transaction_read_only=on`):

  ```sql
  SELECT payload->>'source', payload->>'ecosystem', count(*)
  FROM fact_records
  WHERE fact_kind = 'vulnerability.affected_package' AND NOT is_tombstone
  GROUP BY 1, 2;
  ```

  Result: 3,248 facts, all `source = osv`, ecosystems npm, pypi, maven,
  composer, gomod, rubygems, nuget, cargo. A second query for
  `ecosystem = 'os'` returned 0 rows, so that environment holds no OS advisory
  facts at all.

The fix is therefore dormant until a collector emits an OS vendor-sourced
`affected_package` fact. Other deployments that
enable the Debian, Ubuntu, or Alpine OSV targets: NOT_CHECKED (the same code
argument applies: their facts are untagged OSV facts).

## Known limitation: the release line is not checked

Arbiter ruling (arbiter-eshu, 2026-11-03), under AGENTS.md Mandatory Startup,
quoted in the PR for #9929 and in #9945. The owner may re-grade, and the
re-grade stands.

A vendor-sourced fact is trusted as scoped to the host's release line, but
nothing in the join compares the line. `AffectedPackage` has no release field for
a vendor fact, the purl reader drops the `distro` qualifier, and
`osPackageMatchesAffectedPackage` checks the OS family, not the line. Two
releases of one distribution can carry the same version string, so a list written
for one release can name a host on another. Base already trusted an unscoped
vendor-alone list the same way; this change widens that trust to beat an OSV
single fix. It is not reachable today (census above). Closing it needs line
evidence on the fact, a contract decision for a producer that does not exist yet,
so it is tracked in #9945, which must merge before any producer of an OS
vendor-sourced `affected_package` fact ships.

## Proof

| Check | Result |
|---|---|
| Regression rows written first | RED: 4 tests failed on base (`TestUntaggedVendorVersionListBeatsOSVSingleFix`, `TestVendorVersionListBeatsOSVRanges`, `TestVendorVersionListEdgeCases`, `TestVendorVersionListUnreadableEntries`), exit 1. GREEN after the change, exit 0. |
| Mutant: early vendor-list return disabled | RED in `TestVendorVersionListBeatsOSVRanges`, `TestVendorVersionListUnreadableEntries`. |
| Mutant: unreadable entry on a release-evidence fact ignored | RED in `TestVendorVersionListUnreadableEntries` (only-unreadable and unreadable-before-non-match rows). |
| Mutant: unreadable entry counts as a match | RED in `TestVendorVersionListUnreadableEntries`. |
| Mutant: early check reads facts without release evidence | RED in `TestOSVVersionListBeatsSingleFixShortcut`, `TestFleetOSAdvisoryClassifiesOneFindingPerDigest`, `TestUnlistedReleaseVerdict`. |
| Mutant: an unreadable entry on an untagged OSV fact blocks the shortcut | RED in `TestUntaggedOSVUnreadableEntryKeepsEarlierRule` (dpkg and apk). |
| Mutant: the rpm path passes the APK exact reason as its affected reason | RED in `TestVendorVersionListBeatsOSVRangesRPM` (list names the host, host among other versions). The rpm rows live in `os_vendor_list_precedence_rpm_test.go`: rpm joins the shared decision only when some fact carries an ECOSYSTEM range. |
| Mutant: guard skips release-evidence facts again | RED in `TestVendorVersionListUnreadableEntries`. |
| Mutant: early check moved after the verdict switch | RED in `TestVendorVersionListBeatsOSVRanges`. |

## Performance

Performance Evidence: this path is per-CVE, per-host, in-memory
classification. The change adds one pass over the joined facts'
`AffectedVersions` (one `compare` call per entry) before the verdict switch. No
backend call, queue stage, lock, or graph write is added.

No-Regression Evidence: `BenchmarkOSVendorListPrecedence` (3 joined facts, 32
vendor list entries that do not name the host, the worst case for the new pass)
on darwin/arm64, Go 1.26.10, three 1 s samples, 0 B/op and 0 allocs/op both
ways: with the new pass 6575 / 6541 / 6386 ns/op; with the pass disabled (the
guard still scans the lists) 3333 / 3449 / 3349 ns/op. The added cost is about
0.1 µs per list entry, bounded by the list length, with no allocation. The
host was not quiet-checked for this run; the rows are sequential, not
interleaved, so read them as an order of magnitude, not a speedup claim.

Observability Evidence: No-Observability-Change: the finding's reason stays one
of the existing closed reasons (`dpkg_exact_affected_version`,
`apk_exact_affected_version`, `malformed_advisory_range`, and the unchanged
others). The existing finding status and reason counters and the persisted
finding carry the verdict; an operator finds a moved finding by its reason.
