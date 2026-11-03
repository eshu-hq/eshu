# Admit four advisory sources and gate mirror mode behind a measurement (#9872)

Status: Proposed. The arbiter reviews it before it is accepted. This record
approves no code, schema change, or deployment. Four owner questions stay open
and are marked `OWNER_DECISION_PENDING`.

**Decision.** Phase 4 goes first and adds the CVE List v5, the GHSA database,
ENISA EUVD, and Exploit-DB as query-driven sources in the extracted
`eshu-collector-vulnerability-intelligence` repository, each in one evidence
class from the plan. Mirror mode (Phase 5) is decided here only as a set of
rules, and stays closed until item 5.1 measures shard-scoped generations.

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
- #603 (advisory database lifecycle: cache, mirror, offline, update-only) is
  closed. Its artifact model is in the code: `vulnruntime/source_cache.go` sets
  `cache_artifact_version`, `cache_snapshot_digest`, `cache_updated_at`, and
  `cache_freshness` on `vulnerability.source_snapshot`.
- `vulnerability.source_snapshot` has no license or attribution field today
  (`sdk/go/factschema/vulnerability/v1/source_snapshot.go`).
- Measurements and license findings are in the
  [evidence file](advisory-intelligence-evidence.md), section 3, measured
  2026-10-26.

## Options

| Option | Cost | Verdict |
| --- | --- | --- |
| A. Decide Phase 4 sources only; leave mirror mode for a later ADR | Phase 5 rules get decided twice | Rejected: the shard and cache rules constrain how Phase 4 caches are written, so they belong here |
| B. Decide sources and mirror rules; gate Phase 5 on item 5.1 | One longer record | Chosen |
| C. Build mirror mode now | A 414k-record scope does not fit one generation, one claim, or the in-memory source cache | Rejected: unproven theory |

## Decision

### 1. Source table

Classes are from plan section 4.1: A package-range advisory, B product or CPE
advisory, C CVE-keyed enrichment, D suppression and retraction. "Budget" is the
ceiling the collector enforces per claim; upstream limits are quoted from the
evidence file. A cell marked NOT_CHECKED has no confirmed upstream figure, and
the Phase 4 issue measures it before it ships.

| Source | Class | Kinds | Cadence | Rate budget | License |
| --- | --- | --- | --- | --- | --- |
| CVE List v5, per CVE for owned CVEs (4.1) | C, D; some B | `vulnerability.cve` (new `source` value `cve_list`), `affected_product`, `reference`, `source_snapshot`; CNA blocks with `packageURL` also feed `affected_package` (A) | 10 to 15 min on the `deltaLog.json` cursor for owned CVEs; git is the fallback | One request per owned CVE that changed, at most 1 request per second, with a request budget per claim. Upstream limit NOT_CHECKED | CVE List terms permit redistribution with the MITRE notice |
| GHSA database (4.2) | A, C | `vulnerability.cve` (`source` value `ghsa`), `affected_package`, `reference`; malware advisories as `cve` with their `MAL` or `GHSA` id | 15 to 30 min on the REST `updated` cursor | Token required. Stay under half of 5,000 requests per hour (2,500). Unauthenticated use (60 per hour) is not supported | CC-BY 4.0; attribution carried on the snapshot |
| ENISA EUVD (4.3) | C, D | `vulnerability.cve` (`source` value `euvd`, severity and aliases), `epss_score` (percent converted to a fraction), `known_exploited` (EU KEV as its own source, never merged into CISA KEV) | 1 to 2 h on `fromUpdatedDate`, overlapping one day | At most 1 request per second. Upstream limit NOT_CHECKED | NOT_CHECKED; confirm the EUVD terms before 4.3 ships |
| Exploit-DB (4.4) | C | New kind `vulnerability.exploit_reference`: EDB id, CVE codes, verified flag, and nothing else | Daily | One conditional GET of the repository CSV per day (about 10 MB). Never the website | GPL v2 repository; website terms forbid copying. Blocked until sign-off (question 3) |

Each source also gets, as the phases file already requires: an envelope with a
source-namespaced key, a source enum entry and validation, an entry in the
reducer source-rank table and the readiness source map, registry and schema
overrides, and an operator signal for freshness.

Evidence class sets the ceiling for what a record may do:

- Class A creates a finding with the `affected_exact` or `possibly_affected`
  ceiling.
- Class B never creates a finding alone.
- Class C changes priority only and never creates a finding.
- Class D can lower or retract a finding.

### 2. Kind fit

- CVE List, GHSA, and EUVD fit the existing `vulnerability.*` kinds with new
  `source` values. No new kind is needed for them.
- CVE List SSVC data needs a new optional field on `vulnerability.cve`. Item
  4.1 settles the shape through `eshu-contract-rigor`.
- Exploit-DB needs the new kind `vulnerability.exploit_reference`.
- Alias, related, and upstream edges from item 3.1 (#9954) must land before
  GHSA and EUVD records can join by alias. Phase 4 follows Phase 3 for that
  reason (see the phases diagram).
- Add an optional `license` and `attribution` field pair to
  `vulnerability.source_snapshot` before any new source ships, so that every
  snapshot carries its license (item 4.0 below).

### 3. Cache and mirror lifecycle, reconciled with #603

The Phase 4 sources reuse the #603 cache artifact model unchanged: version,
content digest, updated-at, and freshness on the snapshot. Mirror mode adds
rules on top and does not replace it.

1. **Baseline artifact.** A mirror baseline (CVE List midnight zip with its
   sha256 and cutoff, OSV bulk per ecosystem) is one cache artifact. Its digest
   is `cache_snapshot_digest`. Retention follows #603: keep the latest and the
   previous baseline, and delete older ones.
2. **Stream, do not hold.** The source cache holds whole envelope slices in
   memory today (plan section 4.4). A baseline streams to disk and is read per
   shard. A baseline never loads into memory whole.
3. **Shard-scoped generations.** The shard is the unit of generation: CVE List
   by its `cves/<year>/<NNxxx>` directory, OSV by ecosystem plus id prefix. A
   delta (`deltaLog.json`, `modified_id.csv`) marks shards dirty. Each dirty
   shard is re-collected in full from the baseline, so retraction stays correct
   and the successor guard stays armed. This is the plan 4.4 theory. It is not
   accepted until item 5.1 proves it.
4. **No live fetch in matching.** Update-only refresh stays separate from
   impact recomputation, as #603 requires.
5. **License filter.** A mirror excludes OSV Chainguard and Wolfi records
   (CC-BY-NC-ND 4.0, 62.1% of OSV records) unless an operator opts in
   (question 4).

### 4. Gate for Phase 5

Phase 5 does not start until item 5.1 reports, on representative data: facts
per shard, reducer cost per dirty shard, a replayed full day of deltas, and
whether a per-shard intent hits the bounded evidence load or
`supplyChainImpactTruncation`. Item 5.1 states its decision, threshold, and
deadline before it measures.

### 5. Owner questions (defaults apply until answered)

Each question is `OWNER_DECISION_PENDING`. The default below is the arbiter's
recommendation and is what the Phase 4 issues build against.

| # | Question | Default until answered |
| --- | --- | --- |
| 1 | Are unreviewed GHSA records in scope? | No. Reviewed and malware records only. Unreviewed records are published automatically from NVD with no validity check and are absent from OSV |
| 2 | May Eshu show EPSS, and under what attribution text? | Keep the existing per-CVE API client. Build no new EPSS daily-CSV client until FIRST confirms the text. The attribution wording is NOT_CHECKED |
| 3 | Exploit-DB license sign-off (GPL v2 repository) | Item 4.4 does not start. When it does, facts carry metadata only and never exploit code or description text |
| 4 | Opt in to NC-ND OSV ecosystems? | No. A mirror excludes them. Query-driven matching of owned packages is unchanged |

## Consequences

Good:

- Phase 4 items 4.1 to 4.5 have an agreed class, kind set, cadence, budget, and
  license for each source.
- The cache lifecycle has one model for query-driven and mirror use.
- No mirror code is written before its theory is measured.

Bad:

- The unreviewed GHSA, EPSS, and Exploit-DB defaults narrow Phase 4 until the
  owner answers.
- Rate budgets for CVE List and EUVD are proposals. Upstream limits are
  NOT_CHECKED and need a measurement in 4.1 and 4.3.
- Adding license fields to `source_snapshot` is a contract bump that must
  serialize with item 3.1 on the registry (zone F).

## Implementation plan

Core side stays in `eshu` (reducer, contracts, readiness, query). Collectors
land only in `eshu-collector-vulnerability-intelligence`.

| Item | Work | Starts when |
| --- | --- | --- |
| 4.0 | Optional `license` and `attribution` on `vulnerability.source_snapshot` (minor contract version) | After item 3.1 lands, to serialize registry edits |
| 4.1 | CVE List v5 per owned CVE (#9880) | 4.0 |
| 4.2 | GHSA database (token, reviewed and malware) | 3.1 and 4.0 |
| 4.3 | ENISA EUVD | 3.1 and 4.0 |
| 4.4 | Exploit-DB references (#9881) | Question 3 answered |
| 4.5 | Priority tiers (#9882) | 4.1 to 4.4 kinds exist |
| 5.1 | Shard-scoped generation measurement | 4.1 |
| 5.2 to 5.4 | Baseline, bulk OSV with license filter, catalog surfaces | 5.1 passes; 5.4 needs the owner's scope decision |

Operator signals each new source ships: a freshness gauge per source, a
request-budget-used counter per claim, and a held-stale gauge once the
successor guard covers its scope (item 1.4).

## Acceptance criteria

- [ ] The source table has one row per source with class, kinds, cadence, rate
  budget, and license.
- [ ] The mirror lifecycle names its #603 fields and the shard theory (plan
  section 4.4).
- [ ] The arbiter ruling is recorded in `docs/internal/evidence/`.
- [ ] The four owner questions are listed with defaults.
- [ ] The epic's Phase 4 and 5 rows name the issues this unlocks.

No-Regression Evidence: this record changes no runtime behavior, wire
contract, or gate.

No-Observability-Change: this record adds no metric, span, log key, or status
field. The signals above are planned, not shipped.
