# Admit CVE List, GHSA, and EUVD, hold Exploit-DB for sign-off, and gate mirror mode behind a measurement (#9872)

Status: Accepted with changes (arbiter ruling 2026-11-03,
[the ruling](../evidence/9872-new-source-adr-arbiter-ruling.md)). This record
approves no code, schema change, or deployment. Four owner questions stay open
and are marked `OWNER_DECISION_PENDING`.

**Decision.** Phase 4 goes first and adds the CVE List v5, the GHSA database,
and ENISA EUVD as query-driven sources in the extracted
`eshu-collector-vulnerability-intelligence` repository, each in the evidence
classes named below. Exploit-DB is admitted only after the owner's license
sign-off. Mirror mode (Phase 5) is decided here as provisional rules that bind
nothing until item 5.1 measures shard-scoped generations.

## Context

- The arbiter ruled on 2026-10-26 that new sources and mirror mode need an
  ADR, then land only in the extracted repository. The second condition is met:
  #8870 is closed.
- The collector scoped rules forbid crawling a full public corpus and turning
  public records into graph writes. The EPSS daily CSV, the CVE List and OSV
  mirrors, and any graph projection fall under this record.
- Eshu matches by querying for owned packages, which keeps fact volume tied to
  what Eshu owns. A mirror of everything (414k CVE records, 2.56M OSV records)
  is a different product surface. Plan section 4.4 holds the theory
  (shard-scoped generations); it is unproven.
- #603 (advisory database lifecycle) is closed. Its code is
  `vulnruntime/source_cache.go`. It writes one JSON artifact per target key
  with the envelopes inline, read whole, and annotates the snapshot with
  `cache_artifact_version`, `cache_snapshot_digest`, `cache_updated_at`,
  `cache_freshness`, `cache_expires_at`, and `cache_mode`. The typed
  `source_snapshot` struct carries the first four only.
  `source_cache_cleanup.go` deletes artifacts older than a retention duration
  and refuses to read an artifact over 16 MiB.
- `vulnerability.source_snapshot` has no license or attribution field today.
- The reducer attributes `known_exploited` to `cisa_kev`, `epss_score` to
  `first_epss`, and `affected_product` to `nvd` by kind
  (`go/internal/reducer/supplychain/core/derived_partition.go`
  `advisoryMatchesTrustedSource`, `advisorySourceClass`). None of the three
  payloads has a `source` field. A second emitter of any of them is
  mis-attributed.
- The reducer allows only known source classes
  (`supportedTrustedAdvisorySourceClass`): `ghsa` is there, and `cve_list`,
  `euvd`, and `exploit_db` are not. An unknown class fails the read.
- Measurements and license findings are in the
  [evidence file](advisory-intelligence-evidence.md), section 3, measured
  2026-10-26. The registry holds 12 `vulnerability.*` kinds; the plan and the
  issue count 11.

## Options

| Option | Cost | Verdict |
| --- | --- | --- |
| A. Decide Phase 4 sources only; leave mirror mode for a later ADR | Plan section 4.7 names new sources and mirror mode as one ADR, and #9872 asks for the #603 and shard reconciliation | Rejected |
| B. Decide sources and provisional mirror rules; gate Phase 5 on item 5.1 | One longer record | Chosen |
| C. Build mirror mode now | A 414k-record scope does not fit one generation, one claim, or the whole-artifact source cache | Rejected: unproven theory |

## Decision

### 1. Source table

Classes are from plan section 4.1: A package-range advisory, B product or CPE
advisory, C CVE-keyed enrichment, D suppression and retraction. "Budget" is the
ceiling the collector enforces per claim; upstream limits are quoted from the
evidence file. A cell marked NOT_CHECKED has no confirmed upstream figure, and
the Phase 4 issue measures it before it ships. Every source also emits
`vulnerability.source_snapshot`.

| Source | Class | Kinds | Cadence | Rate budget | License |
| --- | --- | --- | --- | --- | --- |
| CVE List v5, per owned CVE (4.1) | A, C, D | `vulnerability.cve` (new `source` value `cve_list`), `affected_package` for CNA `packageURL` blocks, `reference`. No `affected_product` until that kind carries a source | 10 to 15 min on the `deltaLog.json` cursor for owned CVEs; git is the fallback | One request per changed owned CVE, at most 1 per second, with a request budget per claim. Upstream limit NOT_CHECKED | CVE List terms permit redistribution with the MITRE notice |
| GHSA database (4.2) | A, C | `vulnerability.cve` (`source` value `ghsa`), `affected_package`, `reference`. Malware advisories carry GHSA ids; OpenSSF `MAL` ids come through OSV | 15 to 30 min on the REST `updated` cursor | Token required. At most 2,500 requests per hour, half of the 5,000 token limit. Unauthenticated use (60 per hour) is not supported | CC-BY 4.0; attribution carried on the snapshot |
| ENISA EUVD (4.3) | C, D | `vulnerability.cve` (`source` value `euvd`: severity and aliases) and `vulnerability.known_exploited` (`source` value `euvd`, the EU KEV list as its own exploited source; see section 2). No `epss_score`: its `epss` is FIRST's number as a percent with no `model_version`, so it adds a duplicate and no source | 1 to 2 h on `fromUpdatedDate`, overlapping one day | At most 1 request per second. Upstream limit NOT_CHECKED | NOT_CHECKED; confirm the EUVD terms before 4.3 ships |
| Exploit-DB (4.4) | C | New kind `vulnerability.exploit_reference`: EDB id, CVE codes, verified flag, and nothing else | Daily | One conditional GET of the repository CSV per day (about 10 MB). Never the website | GPL v2 repository; website terms forbid copying. Blocked until sign-off (question 3) |

Each new source also needs: an envelope with a source-namespaced key, a source
enum entry and validation, registry and schema overrides, and an operator
signal for freshness. Reducer, storage, and workflow edit points found so far,
which item 4.1 re-checks and completes for the first new source:

- `supportedTrustedAdvisorySourceClass` and `advisorySourcePriority` in the
  reducer (`derived_partition.go`, `provenance_selection.go`): `ghsa` is
  allowed, while `cve_list`, `euvd`, and `exploit_db` are not, and an unknown
  source ranks last (999).
- `authorizedImpactExpectedKinds` in
  `go/internal/storage/postgres/supply_chain_impact_authorized_advisory.go`
  knows only `nvd`, `osv`, `cisa_kev`, and `first_epss`.
- `decodeVulnerabilityAdvisorySourceClass` hardcodes `known_exploited` to
  `cisa_kev`; the `source` field on `known_exploited` changes this seam too.
- Readiness: `fetchConfirmedSources` in `impact/readiness_source_age.go` and the
  scope grammar in `go/internal/vulnsource/scope.go`. A source stored under a
  token outside that grammar is invisible to readiness.
- The target-source switch in `go/internal/workflow/vulnerability_intelligence_config.go`.

Whether the readiness source map tolerates an unknown source is NOT_CHECKED.

Evidence class sets the ceiling for what a record may do:

- Class A creates a finding with the `affected_exact` or `possibly_affected`
  ceiling.
- Class B never creates a finding alone.
- Class C changes priority only and never creates a finding.
- Class D can lower or retract a finding.

### 2. Kind fit

- CVE List, GHSA, and EUVD fit `vulnerability.cve` with new `source` values.
- EU KEV is its own exploited source and must not merge into CISA KEV. Item 4.3
  takes this route: an optional `source` field on
  `vulnerability.known_exploited`, with a reducer read that keys on it,
  regression test first. The alternative, an `exploited` field on the `euvd`
  `cve` record, is rejected because item 4.5 ranks "each KEV source" and needs
  one kind to read. This is a minor contract bump on `known_exploited`.
- CVE List SSVC data needs a new optional field on `vulnerability.cve`. That is
  a second additive change to the same kind as item 3.1 (#9954), so it follows
  3.1 to keep the SDK release candidates in order. REJECTED and `replaced_by`
  need no new field: item 1.5 shipped them.
- Exploit-DB needs the new kind `vulnerability.exploit_reference`.
- Add an optional `license` and `attribution` field pair to
  `vulnerability.source_snapshot` before any new source ships (item 4.0). Also
  add the two cache fields the struct omits (`cache_expires_at`, `cache_mode`).

### 3. Cache and mirror lifecycle, reconciled with #603

Phase 4 sources use the #603 per-claim cache artifact unchanged. Mirror mode is
provisional and adds these rules:

1. **Baseline artifact.** A mirror baseline (CVE List midnight zip with its
   sha256 and cutoff, OSV bulk per ecosystem) cannot be a #603 artifact: those
   are one inline JSON read whole and capped at 16 MiB at cleanup, and the
   CVE List baseline is 643 MB (the OSV `all.zip` is 2.8 GB). The baseline is a new artifact version or store that
   reuses the four snapshot fields (version, digest, updated-at, freshness).
   Its digest is `cache_snapshot_digest`.
2. **Retention.** #603 retention is a duration cutoff. "Keep the latest and the
   previous baseline" is a new rule for the baseline store, not a #603 rule.
3. **Stream, do not hold.** A baseline streams to disk and is read per shard.
   It is never loaded whole.
4. **Shard-scoped generations.** The shard is the unit of generation: CVE List
   by its `cves/<year>/<NNxxx>` directory, OSV by ecosystem plus id prefix. A
   delta (`deltaLog.json`, `modified_id.csv`) marks shards dirty. Each dirty
   shard is re-collected in full from the baseline, so retraction stays correct
   and the successor guard stays armed. This is the plan 4.4 theory and is not
   accepted until item 5.1 proves it.
5. **No live fetch in matching.** Update-only refresh stays separate from
   impact recomputation, as #603 requires.
6. **License filter.** A mirror excludes OSV Chainguard and Wolfi records
   (CC-BY-NC-ND 4.0, 62.1% of OSV records) unless an operator opts in
   (question 4).

### 4. Gate for Phase 5

Phase 5 does not start until item 5.1 reports, on representative data: facts
per shard, reducer cost per dirty shard, a replayed full day of deltas, and
whether a per-shard intent hits the bounded evidence load or
`supplyChainImpactTruncation`. Item 5.1 states its decision, threshold, and
deadline before it measures. Items 5.2 to 5.4 implement an unmeasured theory and
are not filed until 5.1 passes; 5.4 also needs the owner's catalog scope.

### 5. Owner questions (defaults apply until answered)

Each question is `OWNER_DECISION_PENDING`. The default is the arbiter's
recommendation, and the Phase 4 issues build against it.

| # | Question | Default until answered |
| --- | --- | --- |
| 1 | Are unreviewed GHSA records in scope? | No. Reviewed and malware records only. Unreviewed records are published automatically from NVD with no validity check and are absent from OSV. Whether they carry package ranges is NOT_CHECKED. If admitted later they are class B or C, never A |
| 2 | May Eshu show EPSS, and under what attribution text? | Keep the existing per-CVE API client and build no daily-CSV client. FIRST's FAQ says attribution is requested, and its data page says the API is for lookup, not bulk access (fetched 2026-11-03). The attribution wording is the owner's call; the exact text on FIRST's pages and its copyright and terms pages are NOT_CHECKED |
| 3 | Exploit-DB license sign-off (GPL v2 repository) | Item 4.4 (#9881) does not start and carries a blocked-on-question-3 note. When it does, facts carry metadata only, never exploit code or description text |
| 4 | Opt in to NC-ND OSV ecosystems? | Split. (a) Mirror opt-in: No. (b) Eshu already ingests and derives findings from Wolfi and Chainguard records today (`provenance_selection.go`, `security-intelligence-source-coverage.md`), and osv.dev lists them as CC-BY-NC-ND 4.0. That existing query-driven use is a disclosed exposure the owner and legal sign-off must cover. No coverage is removed on a theory. Whether a Phase 5 catalog surface triggers the share-alike terms of Ubuntu and Alpine (CC-BY-SA 4.0) is NOT_CHECKED |

## Consequences

Good:

- Phase 4 items have an agreed class, kind set, cadence, budget, and license for
  each source, and the reducer attribution collision is named before a collector
  is written.
- One cache model covers query-driven use; mirror use has explicit extra rules.
- No mirror code is written before its theory is measured.

Bad:

- The unreviewed GHSA, EPSS, and Exploit-DB defaults narrow Phase 4 until the
  owner answers.
- Rate budgets for CVE List and EUVD are proposals with NOT_CHECKED upstream
  limits.
- Four contract changes touch `vulnerability.*` kinds in sequence (3.1, 4.0,
  the SSVC field, `known_exploited.source`), so they serialize on the registry.

## Implementation plan

Core side stays in `eshu` (reducer, contracts, readiness, query). Collectors
land only in `eshu-collector-vulnerability-intelligence`.

| Item | Work | Starts when |
| --- | --- | --- |
| 4.0 | Optional `license`, `attribution`, `cache_expires_at`, and `cache_mode` on `vulnerability.source_snapshot` (minor bump) | Now. It touches a different kind than 3.1, so a registry conflict is mechanical |
| 4.1 | CVE List v5 per owned CVE (#9880). It reads REJECTED and `replaced_by` through the contract fields item 1.5 already shipped | 4.0, and 3.1 (#9954) so the SSVC field lands as a later bump on `vulnerability.cve` instead of colliding with 3.1 |
| 4.2 | GHSA database (token, reviewed and malware) | 3.1 and 4.0 |
| 4.3 | ENISA EUVD, with `source` on `known_exploited` | 3.1, 4.0, and the EUVD terms check |
| 4.4 | Exploit-DB references (#9881) | Question 3 answered |
| 4.5 | Priority tiers (#9882) | The 4.1 to 4.4 kinds exist |
| 5.1 | Shard-scoped generation measurement | 4.1, so it runs the real `cve_list` path |
| 5.2 to 5.4 | Baseline, bulk OSV with license filter, catalog surfaces | 5.1 passes; 5.4 needs the owner's scope decision |

Item 4.2 states how a direct GHSA source and OSV records that carry GHSA ids dedupe,
since the reducer maps both to `ghsa`.

Item 4.1 also names its per-CVE endpoint and auth before it ships, and derives
the owned-CVE set the way item 2.1 does (CVEs on active findings, at most 100
per target). It must not fetch through `api.github.com` without a token, since
1 request per second breaks the 60-per-hour unauthenticated limit.

Operator signals each new source ships: a freshness gauge per source, a
request-budget-used counter per claim, and a held-stale gauge once the successor
guard covers its scope (item 1.4).

## Acceptance criteria

- [x] The source table has one row per source with class, kinds, cadence, rate
  budget, and license.
- [x] The mirror lifecycle names its #603 fields and the shard theory (plan
  section 4.4).
- [x] The arbiter ruling is recorded in `docs/internal/evidence/`.
- [x] The four owner questions are listed with defaults.
- [ ] The epic's Phase 4 and 5 rows name the issues this unlocks.

No-Regression Evidence: this record changes no runtime behavior, wire
contract, or gate.

No-Observability-Change: this record adds no metric, span, log key, or status
field. The signals above are planned, not shipped.
