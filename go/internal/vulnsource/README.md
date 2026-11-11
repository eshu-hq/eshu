# Vulnerability Source State

This package defines the shared, durable state contract for bounded
vulnerability intelligence source targets. It deliberately carries no storage
or runtime dependencies.

The state distinguishes configured source targets that are pending, fresh,
stale, rate limited, failed, or partial. Collectors populate the contract from
claimed source attempts, Postgres persists it with keyed upserts, and status/API
surfaces render it so empty result sets are not confused with missing source
configuration.

A derived target's state also carries `PackageKeys`, the canonical package
identities it queried, with `ReplacePackageKeys` telling the store to replace
the scope's stored keys in the same upsert. Readiness matches requested
packages against those keys rather than parsing scope ids.

## Scope ids and sources

`SourceForScopeID` is the one parser from a vulnerability scope id to the
`source` token stored in `vulnerability_source_states` (#9420). The grammar is
core-owned: the workflow coordinator mints `vuln-intel://osv/<ecosystem>/...`,
`vuln-intel://nvd/_window/<start>-<end>`, and
`vuln-intel://first_epss/_findings/<chunk>`, and configured targets use
`vuln-intel://nvd/...`, `vuln-intel://cisa/kev`, `vuln-intel://first/epss`,
`vuln-intel://glad/<ecosystem>`, `vuln-intel://ghsa/<reviewed|malware>`
(GHSA admits reviewed and malware advisories only; `unreviewed` returns
`ok=false`), the coordinator-minted per-window form
`vuln-intel://ghsa/<reviewed|malware>/_window/<start_unix_us>-<end_unix_us>`
(#10037; `ParseGHSAWindowScope` refuses everything outside that grammar,
and `GHSAStreamScopeID` maps a window back to its anchor for stream-keyed
source state), and `vuln-intel://cve_list/cve` for
explicit owned CVE IDs (CVE List v5, #9880; whole-corpus reads have no
scope). It returns `ok=false` for anything else,
so a writer never invents a token. Tests in `coordinator/vulnerability` and
`query/supply/chain/impact` pin it against the coordinator's format strings
and the readiness reader's scope table. The coordinator test lists one row per
derived scope shape and fails when a non-test file in `coordinator/vulnerability`
has a `vuln-intel:` string literal whose authority has no row or cannot be read
as a literal, so a new authority cannot ship unmapped (#10009). It does not
catch a new path shape under an authority that already has a row, or a scope
minted outside that package: add the row by hand when you mint one.
`DefaultFreshnessWindow` (14 days) is the shared fresh-versus-stale window for
the in-tree collector and the extension host recorder.

## Coordinator-minted scope shapes

The extension host and the coordinator read these shapes without a collector:

- `ParseDerivedOSVSlotScope` and `IsDerivedOSVSlotScope` validate a pinned slot
  scope, `vuln-intel://osv/<ecosystem>/_slot/<vslot>/<chunk>` (#9432). The
  queries behind it ride the run's `requested_scope_set`, never the scope id.
- `DerivedScopePackageKeys` returns the package keys of a single-package or
  `_batch` derived OSV scope, with the claim path's bounds: 100 queries, blanks
  and repeats dropped. `SlotScopePackageKeys` does the same for a slot claim's
  resolved names. An ecosystem `packageidentity` cannot normalize yields no keys,
  so the scope reads as unmatched rather than guessed.
- `NVDWindowScopeID` and `ParseNVDWindowScope` mint and read
  `vuln-intel://nvd/_window/<startMicros>-<endMicros>` (#9971). Same bounds mint
  the same id, and degenerate bounds mint `""`. `NormalizeNVDWindowTime` is UTC
  truncated to the millisecond, the form the coverage descriptor persists.

`derivedOSVVirtualSlotCount` duplicates `workflow.OwnedPackageVirtualSlotCount`
so this package stays free of the reducer tree behind `workflow`; a test pins the
two together.

These functions do no I/O and hold no state, so they add no telemetry of their
own.

## GHSA rolling activation

Core admits a `ghsa` target only as a rolling anchor under
`derive_ghsa_rolling_windows` and refuses it otherwise (#10516), so a static
GHSA claim is unreachable by configuration. The section is off by default:
nothing plans a `ghsa` claim unless an operator configures a `ghsa` target
with the section enabled. GHSA rolling is activatable for production truth
via `derive_ghsa_rolling_windows` with a host-minted token since the S6
1-hour tokened QA run went green
([eshu-hq/eshu#10465](https://github.com/eshu-hq/eshu/issues/10465),
[S6 record](https://github.com/eshu-hq/eshu/issues/10465#issuecomment-6789735481)).
A production coordinator configuration must enable the section on any `ghsa`
target.

- The direct GHSA source is excluded from the protected scanner read:
  `reconcile_public_advisories_v1` refuses a `ghsa` target with
  `public advisory target source unsupported`, and
  `authorizedImpactExpectedKinds` has no `ghsa` entry. GHSA ids that OSV carries
  keep flowing through registered OSV scopes.
- Core has no `ghsa` source-terms row yet. It follows the factschema pin: the row
  lands after core moves to factschema rc.17, and takes its license and attribution
  from that release's `source_terms_golden.json`.
- Readiness accepts a `ghsa` target as well-formed: `SourceForScopeID` maps
  `vuln-intel://ghsa/reviewed` and `vuln-intel://ghsa/malware` to `ghsa`, and the
  readiness query's source list names it, so a configured target does not make
  readiness reads return `readiness_unavailable`.
- The extracted satellite (`eshu-hq/eshu-collector-vulnerability-intelligence`)
  dispatches the `ghsa` source (supported-source switch and GHSA collect
  path at v0.1.10; rolling window scopes resolve against the configured
  anchor). Core admits a `ghsa` target only as a rolling anchor under
  `derive_ghsa_rolling_windows` and refuses it otherwise
  (`validateVulnerabilityGHSATarget`, #10516); a boundless anchor works
  end to end only where satellite >= 0.1.12 is pinned (the mirror
  #83/#85 shipped in 0.1.12, live in the green S6 1-hour tokened QA run
  under #10465). GHSA rolling is activatable for production truth via
  `derive_ghsa_rolling_windows` with a host-minted token
  ([S6 record](https://github.com/eshu-hq/eshu/issues/10465#issuecomment-6789735481)).

## GHSA and OSV copies of one advisory

The reducer does not deduplicate a direct GHSA fact and an OSV copy of the same
advisory. This matters only once GHSA is activated; a proof test in
`reducer/supplychain/core` is required first.

- `classifyAdvisorySource` maps `osv` with a `GHSA-` id to `ghsa`, so both copies
  are logical source `ghsa` with equal rank.
- The collector's stable fact keys differ by source, so Postgres holds two
  `vulnerability.cve` facts. They load into one group keyed by `cve_id`, or by
  `advisory_id` when the record has no CVE.
- `selectAdvisoryProvenance` sorts by rank, Source, AdvisoryID, WithdrawnAt, then
  Status, stably. Two `ghsa` copies tie on all of these unless withdrawn or status
  differ, so severity, fixed version, and range come from whichever copy arrives
  first in load order. `representative` breaks the same tie by lexical FactID.
- `AdvisorySources` collapses on `Source|AdvisoryID`, so the output lists one
  `ghsa` row. `EvidenceFactIDs` keeps both fact ids.
- GHSA malware ids should not collide: from reading upstream data (not a
  measurement), OSV carries those as `MAL-` ids, classified `osv-malicious`.

The proof test takes one `osv` and one `ghsa` observation of the same GHSA id and
CVE, and asserts one `AdvisorySources` row, both evidence fact ids, and a
deterministic selection. A second case covers an OSV record whose `cve_id` differs
from the GHSA record's, which forms two groups.

## Evidence (#9959 ghsa key spelling)

No-Regression Evidence: `validateVulnerabilityGHSAKeySpelling` in `workflow` reads the raw
document on every `ValidateVulnerabilityIntelligenceCollectorConfiguration` call,
for every source, because a target's source can only be read from its raw keys.
Baseline: the same branch on main `0cb384ca89` with that one call removed. Input:
one document of 64 targets. Run: `go test -bench -benchmem -benchtime=2s -count=3`,
Go 1.26.6, no backend, host load average about 46, so treat times as rough.

| Document | Before ns/op | After ns/op | Before allocs | After allocs |
| --- | --- | --- | --- | --- |
| 64 `ghsa` targets | 184,208 to 197,250 | 1,046,722 to 1,350,277 | 414 | 8,578 |
| 64 `nvd` targets | 100,145 to 106,847 | 496,963 to 549,517 | 414 | 3,843 |

The added cost is under 1.2 ms per validation of that document. It is safe because
it is config validation, not a per-fact path: the coordinator's default reconcile
interval is 30s, and the number of calls per tick was not measured. Key walking is
linear in document size. No Cypher, SQL, queue, lease, claim, batching, or
concurrency code changed, and the check is a pure function of the input string, so
there are no terminal queue or row counts. No migration is added:
`TestPublicAdvisoryReconcileFunctionDoesNotAdmitGHSA` pins that migration 254's
reconcile function still refuses `ghsa`.

No-Observability-Change: core adds no metric, span, or log. A refused `ghsa` target
surfaces through the existing configuration validation error at coordinator and
collector-instance load, which names the key and quotes no value.

## CVE List shard mapping

`CVEListShardForCVEID` maps a CVE ID to its CVE List v5 shard directory,
`cves/<year>/<NNNxxx>`, grouping sequence numbers in batches of 1,000;
`CVEListDirtyShards` reduces delta-touched IDs to the sorted unique shards
needing re-collection, and `CVEListShardGenerations` builds one full-replace
generation per dirty shard with its 1:1 record-to-fact count. This is the
item 5.1 measurement substrate (#9961, plan §4.4): pure string accounting,
no network, database, or workflow mutation. The measured verdict lives in
`docs/internal/evidence/9961-shard-scoped-generations.md`.

## GHSA rolling windows: performance and observability evidence (#10037)

No-Regression Evidence: baseline `origin/main` at `2d477a517d`; after this
change at the branch HEAD. At measurement time GHSA was not activatable: no
configuration section minted a GHSA window scope, so every production claim
then took the unchanged path. (Superseded in part by the
`derive_ghsa_rolling_windows` wiring, #9959/#10516: the section now exists
and mints window scopes when enabled and stays off by default. Superseded in
full by the green S6 1-hour tokened QA run under #10465
([S6 record](https://github.com/eshu-hq/eshu/issues/10465#issuecomment-6789735481)):
GHSA rolling is activatable for production truth via
`derive_ghsa_rolling_windows` with a host-minted token. The measurements
below still describe the non-GHSA hot path.) The added hot-path work per non-GHSA claim is one
`strings.Contains` infix check in `IsGHSAWindowScope` plus one map-free
policy nil check in the extension host; the successor guard gains one
boolean arm evaluated before the existing arms with no new query (the
write-once prior read reuses the candidate tuple and active read the
guard already performs, rechecked in-transaction by the existing #8750
recheck). Measured locally with `go test -count=1` on the touched
packages (Go toolchain on this host, no database): `extensionhost`
0.659s, `coordinator/vulnerability` 0.029s, `vulnsource` under 0.01s for
the grammar and golden-vector tests, and the storage guard fake-harness
cells including the racing-recheck cell pass in the
`internal/storage/postgres` suite. Terminal queue and row counts were
unchanged because no window scope was planned in the measured configuration;
the cursor table adds at most two rows per collector instance (one per
advisory type) now that rolling activation has landed. Live-DSN cursor cells
(`TestGHSAWindowCursorCompareAndAdvanceLive`) are enrolled in the
live-postgres-readiness runner and were NOT_CHECKED locally (no
`ESHU_POSTGRES_DSN` in this environment); they prove overlapping
transactions settle at the max and a rollback leaves the cursor.

Observability Evidence: `eshu_dp_ghsa_cursor_lag_seconds{advisory_type}`
(gauge; a value growing past the anchor refresh interval plus one hour
means the stream is stuck), `eshu_dp_ghsa_window_advance_total{advisory_type,outcome}`
with closed outcomes `advanced`, `prefix`, `held`, `no_progress`, the new
`write_once` label on `eshu_dp_ingestion_successor_guard_decisions_total`,
WARN log `ghsa window claim refused by commit policy, nothing committed`
with the failure class, advisory type, work item, and generation (never
the scope id or a payload value), and the existing `successor_guard`
commit-stage log naming scope, generation, and rule. The conflict domain
is one advisory-type stream's cursor row plus one write-once scope row;
concurrent claims on different windows or types never serialize, and the
compare-and-advance keeps the max under either commit order.
