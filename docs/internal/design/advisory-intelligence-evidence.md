# Advisory Intelligence: Evidence Appendix

Evidence behind
[Advisory Intelligence Correlation Plan](advisory-intelligence-correlation-plan.md).
Measured 2026-10-26. Code cites are against `origin/main` `64d7e60c50`.

Labels: **measured** means a read-only query or HTTP request run that day.
**code** means read in source at the cited line. **NOT_CHECKED** means not
confirmed. QA figures are aggregate counts from the shared QA environment
(build `61cfc94`), taken through the read replica with
`default_transaction_read_only = on` and through the MCP read tools. No
repository, package, or advisory identity from that environment appears here.

## 1. Shared QA environment state

### 1.1 Deployment and configuration (measured)

- The `vulnerability_intelligence` collector deployment runs one instance with
  0 restarts. The coordinator registers it and claims are enabled.
- The instance configuration has one explicit target, `cisa_kev`, plus
  `derive_from_owned_packages {enabled: true, sources: [osv]}`. It sets no
  `ecosystems` and no `target_limit`. There is no `first_epss` target, no
  `nvd` target, and no NVD API key.
- The security-alert (Dependabot) instance was moved to a component-configured
  instance on 2026-10-26. The old instance was deactivated at 13:09Z, and the
  new one had `observation_count: 0` at 19:30Z.

### 1.2 Source state (measured, `vulnerability_source_states`)

| Source | Scopes | Results | Freshness | Last success |
| --- | --- | --- | --- | --- |
| `cisa_kev` | 1 | 1,756 | fresh | 2026-10-26 19:12Z |
| `osv` / npm | 451 | 726 (101 scopes with results) | fresh | 2026-10-22 to 10-26 |
| `first_epss` | 0 | 0 | none | never |
| `nvd` | 0 | 0 | none | never |
| any other OSV ecosystem | 0 | 0 | none | never |

There were 643 completed work items, 0 failed, and 0 generation dead letters.
KEV has 188 generations over about 93 hours, about one every 30 minutes.

### 1.3 Facts and findings (measured)

Active-generation facts: `vulnerability.reference` 4,539,
`known_exploited` 1,756, `affected_package` 801, `cve` 726,
`source_snapshot` 452. Impact findings at the first read (about 19:40Z): 822
from vulnerability-intelligence scopes and 123 from security-alert scopes, 945
in total.

Canonical winners, 645 in total:

| Status | Profile | Rows | CVEs | Repos |
| --- | --- | --- | --- | --- |
| `affected_exact` | precise | 268 | 227 | 37 |
| `not_affected_known_fixed` | precise | 233 | 170 | 27 |
| `possibly_affected` | comprehensive | 144 | 112 | 18 |

The winner scope is an OSV scope for 523 rows and a security-alert scope for
122. A later read (about 20:30Z), after more OSV rotation, found 830 finding
facts in OSV scopes, 123 in security-alert scopes, and 0 in the KEV scope.

Of the 123 security-alert-scope findings, 42 have
`security_alert.repository_alert` in their evidence path. The other 81 are
OSV-derived copies built by the alert scope's run from cross-scope evidence.

Grouping raw finding facts by (repository, package, advisory) gives 614 keys:

| Families | Keys |
| --- | --- |
| both | 27 |
| OSV only | 491 |
| Dependabot only | 96 |

These keys are coarser than the canonical key, so they do not add up to the
645 winners. `source_count` is 1 for 456 winners, 2 for 132, 3 for 3, and 4
for 54.

### 1.4 Correlation gaps (measured)

| Gap | Measurement |
| --- | --- |
| Severity missing | Severity bucket `none` on 506 of 523 OSV-scope winners and 91 of 122 security-alert-scope winners. |
| Severity data present (OSV) | 705 of 726 OSV `vulnerability.cve` facts carry a CVSS vector in `payload.severity[]`. All 726 carry `database_specific.severity`. None carries `cvss_score`, `cvss_vector`, or `severity_label`. |
| Severity data present (alerts) | Of 92 Dependabot alerts, 68 carry a score and vector and all 92 carry a label. 31 of 123 security-alert-scope findings have a score. |
| KEV | 1,756 KEV CVEs. 0 of the 447 distinct finding CVEs and 0 of the 414 advisory ids (CVE ids plus aliases) are in KEV. 0 findings are KEV-flagged. The empty intersection explains the zero. |
| EPSS | 0 of 945 finding facts have EPSS. No EPSS target is configured. 84 of 92 alerts carry GitHub's EPSS, which the impact-finding path drops. The alert-reconciliation path keeps it. |
| GHSA-only identity | 50 of 645 winners have a GHSA id in `cve_id`, covering 46 advisories: 38 from OSV scopes and 8 from alert scopes. 0 of the 46 have a CVE alias in any stored `vulnerability.cve` fact. |
| Readiness | The findings list reported `readiness.evidence_sources: []` and `freshness: unknown` while both sources were fresh. |
| Console | The console loads `limit=100` per status with no paging, so 268 `affected_exact` rows show as 100. Each row is labeled by `advisory_id`, which is a GHSA id on all 645 winners. |

## 2. Code facts

### 2.1 Collection side

Paths below are relative to `go/internal/` unless they start with `go/cmd/`
or `sdk/`.

- The OSV envelope writes only a raw `severity` array (`newOSVCVEEnvelope`
  in `collector/vulnerabilityintelligence/envelope.go`). The NVD envelope
  writes `cvss_score`, `cvss_vector`, and `severity_label`
  (`newNVDCVEEnvelope` in `nvd_envelope.go`).
- `HTTPProvider.collectNVD` (`collector/vulnerabilityintelligence/vulnruntime/provider.go`)
  fetches one page of the modified window and stamps the snapshot
  `Complete: true`. `NVDCVEResult.TotalResults` is decoded and never read.
- The destructive-successor guard runs only when the generation has a
  `FreshnessHint` (`IngestionStore.skipUnchangedGeneration` in
  `storage/postgres/ingestion_backfill_seam.go`, gated by
  `backfill.GenerationNeedsDuplicateCheck`). The vulnerability runtime never
  sets one, so a partial but successful fetch replaces the generation and
  retracts every absent record.
- Derived targets accept only `osv` as a source
  (`validateVulnerabilityTargetDerivation` in
  `workflow/vulnerability_intelligence_config.go`). With no `ecosystems` set,
  `deriveVulnerabilityTargets`
  (`coordinator/vulnerability/intelligence_derivation.go`) falls back to
  `["npm"]`. `DerivedTargetLimit` defaults to 100 package@version queries
  (`defaultDerivedVulnerabilityTargets`), with a ceiling of 5,000
  (`maxDerivedVulnerabilityTargets`).
- EPSS targets need explicit `cve_ids`, at most
  `maxVulnerabilityIntelligenceCVEIDs` (100) per target
  (`validateVulnerabilityIntelligenceTargetConfiguration`). Nothing derives
  them from findings. The NVD window is static configuration parsed once in
  `mapTarget` (`go/cmd/collector-vulnerability-intelligence/config.go`), with
  no rolling lookback.
- No `vulnerability.*` schema has an alias field. There is no fact kind for
  EUVD records or exploit references.
- Cadence is set per instance, not per source. One process serves one
  instance (`selectVulnerabilityInstance` in the same `config.go`).

### 2.2 Reducer and query side

- `supplyChainCVEFromEnvelope`
  (`reducer/supplychain/core/typed_decode.go`) decodes only `CVSSScore`,
  `CVSSVector`, and `SeverityLabel`. `cvssPriorityContribution`
  (`priority.go`) returns nothing when the score is 0.
- The severity bucket is computed in SQL from `payload->>'cvss_score'` only.
  The `CASE` in `supplyChainImpactWinnerSelectSQL`
  (`storage/postgres/supply_chain_impact_canonical_winners_store.go`) must
  stay byte-identical to the read-time query (`aggregates_queries.go`).
  Provenance selection takes severity only from an observation with
  `SeverityScore > 0` (`provenance_selection.go`).
- Group identity is `FirstNonBlank(cve_id, advisory_id)`, in
  `supplyChainCVEFromEnvelope` and `supplyChainAffectedPackageFromEnvelope`.
  The finding reducer and the typed `vulnerability.cve` contract carry no
  aliases. The OSV payload does carry `aliases` and `related`, and the
  evidence route reads them. `canonical_key` and `finding_id` are built in
  two SQL sites that must stay byte-identical: the winners store and
  `query/supply/chain/impact/findings_queries.go`.
- In `addSupplyChainImpactIndexEntry` (`index_build.go`), EPSS takes
  whichever fact is indexed last, keyed by `cve_id`, and never reads
  `score_date`. KEV is a presence flag.
- KEV and EPSS are projector trigger kinds (`candidateFactKinds` in
  `projector/supply/chain/impact/intents.go`). Each enrichment generation
  runs `supply_chain_impact` for its own scope. That run adds the
  enrichment CVEs to `supplyChainImpactFilter` (`core/active_filter.go`),
  loads matching evidence across scopes, and writes its own finding copies.
  The winners table keeps the highest `priority_score` per `canonical_key`.
- `dependencyCatalog` (`reducer/crossscope/dependencies.go`) re-runs
  `supply_chain_impact` across scopes only when `container_image_identity` or
  `ci_cd_run_correlation` complete. Nothing else re-runs it across scopes.
- `appendSecurityAlertImpactFindings` (`core/security_alert.go`) takes the
  score and vector from the alert's `cvss` object and has no EPSS handling.
- `successor_guard.go` (`storage/postgres/backfill/`) defines
  `DecideSuccessor`. It refuses an empty successor over a non-empty prior, and
  a full successor under half the prior. A refusal rolls back and returns
  success to the claim. The PagerDuty collector passes
  `TrustedDeletion: AllowEmptySuccessor`.
- Go reachability: `extractGoAffectedPackages` keys on the advisory id
  first, while `applyGoSupplyChainReachability` looks up by `finding.CVEID`.
  Test fixtures set `cve_id` equal to `advisory_id`, so the mismatch is
  untested (NOT_CHECKED live).
- `version_match.go` dispatches Go, RubyGems, Composer, and Hex matchers. The
  public source-coverage doc still calls them source-only.
- `directEvidenceRuntimeStatus` (`status/collector/runtime.go`) sets the
  detail "status evidence exists without workflow coordinator registration".
  `list_collectors` shows that text next to `coordinator_registered: true`.
- In `buildSupplyChainImpactFindingsWithQuarantine` (`impact.go`), the CPE
  product path runs only when a CVE has no `affected_package` rows.
  `supplyChainAffectedProductFromEnvelope` decodes only `criteria` and
  `vulnerable`, so the NVD `negate` and `operator` flags are dropped.
- The CVE fact schema (`sdk/go/factschema/vulnerability/v1/cve.go`) has no
  status field, so an NVD `Rejected` record is not treated as withdrawn. Only
  OSV maps `withdrawn_at`.
- The `seed_keys` step of `ListEvidenceQuery`
  (`query/supply/chain/advisory/evidence_sql.go`) expands aliases one hop and
  recognizes only the `CVE-`, `GHSA-`, and `OSV-` prefixes.
- Readiness reads fact counts and `vulnerability_source_states` rows. Its
  `target_vulnerability_source_scopes` step
  (`query/supply/chain/impact/readiness_postgres_query.go`) matches source
  states by literal scope ids, so a second EPSS target or a new source does
  not appear in `source_states[]`.
- The finding writes go to Postgres only. The `DomainSupplyChainImpact` entry
  in `reducer/retirement/plan.go` reads "no graph writes".

### 2.3 Console

`loadVulnerabilities` (`apps/console/src/api/eshuConsoleVulnerabilities.ts`)
reads `/api/v0/supply-chain/impact/findings?limit=100` for `affected_exact`
and `affected_derived` only, and labels each row with `advisory_id` first.

## 3. Upstream sources (measured 2026-10-26)

| Source | Records | Bulk | Daily change | How often it actually changes |
| --- | --- | --- | --- | --- |
| CVE List v5 | 414,379 (18,651 rejected) | midnight zip 643 MB; git 3.2 GB | mean 2,648 (179 to 5,947) | git commits, median gap 15.8 min |
| GHSA | 37,219 reviewed; 357,994 unreviewed; 125,176 malware | git 3.6 GB | ~100 reviewed updates on a weekday | REST and GraphQL `updated` filters; git in batches |
| OSV | 2,559,866 | `all.zip` 2.8 GB; `modified_id.csv` 153 MB | 6,167 on a Sunday; bursts to 24,154 per hour | export about every 15 min |
| NVD | 414,390; 2.42 M change events | yearly feeds 232 MB gz | 806 to 5,305 modified | bursty; NIST asks for at most one poll every 2 h |
| ENISA EUVD | 413,244 | no full dump; 12 MB CVE mapping CSV | 88 to 2,670 updated | continuous; built on CIRCL Vulnerability-Lookup |
| CISA KEV | 1,756 | JSON 1.8 MB | ~22 additions per month | changes on about 46% of days |
| FIRST EPSS | 395,492 | 2.8 MB gz per day | full rewrite daily | once per day, about 13:30Z |
| Exploit-DB | 49,449 | CSV 10 MB in GitLab | irregular | 15 commit-days in 73 days |

Constraints from the source research:

- **NVD.** The start-here guidance says to poll no more than once every two
  hours, with a 6 s sleep between requests. The rate limit is 5 or 50 requests
  per 30 s. Enrichment has been risk-based since 2026-04-15: KEV, federal, and
  EO 14028 CVEs come first, and 58,770 CVEs are `Deferred`.
- **CVE List.** The hourly release zips are cumulative since midnight and
  sometimes skip or mislabel an hour. The gap-free tail is git or
  `cves/deltaLog.json` (rolling 30 days).
- **EPSS.** Model v5 (`v2026.06.15`) rescored every CVE on 2026-06-15. Store
  `model_version` with each score and do not compare across versions.
- **EUVD.** `epss` is a percent. EUVD reports 1.03 where FIRST reports
  0.01025 for the same CVE (measured). `aliases` is a newline-joined string and
  does not always list the CVE first. Product versions are free text.
- **OSV.** Chainguard and Wolfi make up 62.1% of records and are licensed
  CC-BY-NC-ND 4.0. Ubuntu and Alpine are CC-BY-SA 4.0. Unreviewed GHSA records
  are absent from OSV (4 of 4 probes).
- **Licenses.** CVE List terms permit redistribution with the MITRE notice.
  GHSA is CC-BY 4.0, KEV is CC0, and NVD is public domain with an attribution
  notice. Exploit-DB's repository is GPL v2, while the website terms forbid
  copying. No formal EPSS license or attribution text was found
  (NOT_CHECKED).
- **Origin of the cadence text.** The quoted cadence comes from cve.now, a
  closed aggregator launched 2026-10-22. Its intervals are its own choice, not
  upstream guidance.
- **GHSA.** Unreviewed advisories are published automatically from the NVD
  feed with no validity check, per GitHub's advisory-database docs. REST
  allows 60 requests per hour unauthenticated and 5,000 with a token.
- **OSV CDN.** Bucket objects are served with
  `cache-control: public, max-age=3600` (measured).

The key primary pages are
`https://nvd.nist.gov/developers/start-here`,
`https://github.com/CVEProject/cvelistV5`,
`https://google.github.io/osv.dev/data/`,
`https://www.first.org/epss/data`,
`https://euvd.enisa.europa.eu/apidoc`, and
`https://www.cisa.gov/known-exploited-vulnerabilities-catalog`.

## 4. How mature tools correlate

- **Identity is derived, not stored as truth.** osv.dev keeps the raw
  aliases on each record and recomputes alias and upstream groups every
  15 minutes. It skips withdrawn records, records with more than 5 aliases,
  and groups larger than 32. `upstream` (a distro record pointing at a CVE) is
  directed and is not an alias. Folding upstream into aliases has caused
  documented over-merging (osv.dev issues 2374 and 888).
- **Language packages only come from package-keyed advisories.** Grype turns
  CPE matching off by default for java, dotnet, golang, javascript, python,
  ruby, rust, hex, dpkg, and rpm. Trivy uses the OS vendor advisory for OS
  packages and adds upstream ranges only in `comprehensive` mode.
- **Enrichment ranks and never creates.** EPSS, KEV, SSVC, and exploit
  references are CVE-keyed. Dependency-Track 5.1 resolves KEV through aliases.
  FIRST says EPSS is not a risk score and should not be multiplied by CVSS.
- **Suppression must match product and vulnerability identity together.**
  CISA's VEX minimum requirements say the absence of a statement implies
  nothing. Grype ignores `fixed` and `not_affected`, and keeps `affected` and
  `under_investigation` as annotations.
- **Recompute cost.** No tool publishes a cost for re-matching after an
  advisory changes. Measure on the Eshu corpus before choosing a mechanism.

No-Regression Evidence: this appendix records measurements and code reads. It
changes no runtime behavior, wire contract, or gate.

No-Observability-Change: this appendix adds no metric, span, log key, or
status field.
