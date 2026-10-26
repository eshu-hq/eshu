# Advisory Intelligence Correlation Plan

Status: Proposed, 2026-10-26. Epic: #9209.

Companion files:

- [Advisory Intelligence Phases](advisory-intelligence-phases.md): the work
  items.
- [Advisory Intelligence Evidence](advisory-intelligence-evidence.md): the
  measurements and code facts.

Related:

- #12: vulnerability intelligence collector.
- #589: advisory model (normalized GHSA, CVE, OSV, NVD, EPSS, KEV, CVSS,
  and CWE evidence).
- #601: multi-source admission.
- #603: source cache.
- #8870: collector extraction, under #6707.
- #8866: security-alert extraction.
- #8862: package-registry extraction.

## 1. Summary

The goal is to turn advisories from the CVE List, GHSA, OSV, NVD, ENISA EUVD,
CISA KEV, FIRST EPSS, and Exploit-DB into findings on the packages, images,
workloads, and deployments Eshu already knows about. Each finding should carry
correct severity, exploit signals, and provenance.

Most of the pipeline exists. Measurements in the shared QA environment show
where it falls short:

- **Coverage.** OSV runs for npm only, because derivation defaults to npm
  when no ecosystems are configured. KEV runs. EPSS and NVD are not
  configured at all.
- **Severity.** 506 of 523 OSV-scope findings have no severity. The OSV
  collector stores the CVSS vector and the GHSA label in fields the reducer
  never reads, and the severity bucket is computed from `cvss_score` alone.
- **KEV.** It matches nothing. None of the 447 CVEs on findings is in the
  KEV list. So far the intersection is simply empty, and npm-only coverage is
  the likely reason.
- **EPSS.** No finding has EPSS, because no EPSS target is configured. The
  Dependabot alerts carry GitHub's own EPSS (84 of 92), but the impact-finding
  path drops it.
- **Console.** Every row is labeled with a GHSA id, there is no source
  column, and the list stops at 100 rows. OSV findings look the same as
  Dependabot findings.

The work runs in six phases, detailed in the phases file:

| Phase | Scope |
| --- | --- |
| 0 | Deployment configuration only |
| 1 | Correctness fixes |
| 2 | Enrichment wiring |
| 3 | Advisory identity |
| 4 | New sources, after an ADR and the #8870 cutover |
| 5 | Snapshot-plus-delta mirror, same gate as Phase 4 |

Collector fixes land in-tree with a port to the extracted repository in the
same wave (§4.7).

## 2. Where things stand

| Area | State | Evidence |
| --- | --- | --- |
| OSV coverage | npm only; other ecosystems are never derived. 268 `affected_exact` winners across all sources, in 37 repos. | Evidence §1.2-1.3 |
| Severity | `none` on 506 of 523 OSV-scope winners and 91 of 122 security-alert-scope winners. Only 42 of the 123 security-alert-scope findings come from an alert; the rest are OSV-derived copies. | Evidence §1.4 |
| KEV | 1,756 ingested. 0 matches, because the intersection with finding CVEs is empty. | Evidence §1.4 |
| EPSS | Not configured. The impact-finding path drops alert EPSS. (The separate alert-reconciliation path keeps it.) | Evidence §1.4, §2.2 |
| NVD | Not configured. When it runs, it reads only one page. | Evidence §2.1 |
| Identity | Findings group by the `cve_id` string. The reducer and the typed contract carry no aliases. The evidence route reads them. 46 advisories behind GHSA-keyed winners have no CVE alias in any stored fact. | Evidence §1.4, §2.2 |
| Retraction | NVD `Rejected` is invisible. A partial successful fetch can retract good records. | Evidence §2.1-2.2 |
| Read surfaces | Readiness reports `freshness: unknown`. The console has no source column or paging. | Evidence §1.4, §2.3 |

## 3. Defects (each needs a failing regression first)

| ID | Defect | Where |
| --- | --- | --- |
| D1 | The OSV envelope fills neither `cvss_vector` nor `severity_label` (both already exist in the contract). | `newOSVCVEEnvelope` (`collector/vulnerabilityintelligence/envelope.go`) |
| D2 | The severity bucket is computed in SQL from `cvss_score` only, and provenance selection requires `SeverityScore > 0`. A record with only a vector or only a label gets no severity, on both the advisory and the alert path. | `supplyChainImpactWinnerSelectSQL`, `aggregates_queries.go`, `provenance_selection.go` |
| D3 | NVD fetches one page and stamps the snapshot `Complete: true`. | `HTTPProvider.collectNVD` (`vulnruntime/provider.go`) |
| D4 | Vulnerability scopes have no successor protection. A partial successful fetch retracts every absent record. | `IngestionStore.skipUnchangedGeneration`, `backfill.DecideSuccessor` |
| D5 | NVD `Rejected` is not treated as withdrawn, and a fully withdrawn group still emits a finding. | `sdk/go/factschema/vulnerability/v1/cve.go`, `core/impact.go` |
| D6 | EPSS is last-write-wins. `score_date` and `model_version` are ignored. | `addSupplyChainImpactIndexEntry` (`core/index_build.go`) |
| D7 | The CPE `negate` and `operator` flags are dropped, so a negated configuration can match. | `supplyChainAffectedProductFromEnvelope` (`core/typed_decode.go`) |
| D8 | Readiness matches NVD, KEV, and EPSS source states by literal scope ids (OSV and GLAD by ecosystem), and findings readiness reports `unknown`. | `target_vulnerability_source_scopes` (`impact/readiness_postgres_query.go`) |
| D9 | The console shows 100 rows per status, no source, and the GHSA id as the label. | `loadVulnerabilities` (`apps/console/src/api/eshuConsoleVulnerabilities.ts`) |
| D10 | Go reachability keys on the advisory id but looks up by the CVE alias. A reproduction is needed. | `extractGoAffectedPackages`, `applyGoSupplyChainReachability` |
| D11 | The source-coverage doc lists Go, RubyGems, Composer, and Hex as source-only, but the matchers exist. | "Advisory Provenance" in `security-intelligence-source-coverage.md`; `version_match.go` |
| D12 | The collector status detail says "without workflow coordinator registration" for registered instances. | `directEvidenceRuntimeStatus` (`status/collector/runtime.go`) |
| D13 | The EPSS on Dependabot alerts never reaches the finding. | `core/security_alert.go` has no EPSS handling |

## 4. Design decisions

### 4.1 Evidence classes

Every source record falls into exactly one class, and the class sets what the
record may do to a finding.

| Class | Sources | May create a finding | Ceiling |
| --- | --- | --- | --- |
| A. Package-range advisory | OSV, GHSA reviewed, GLAD, distro trackers, CNA blocks with `packageURL` or `collectionURL` | Yes | `affected_exact` with an exact installed version and an ecosystem comparator. Otherwise `possibly_affected`. |
| B. Product or CPE advisory | NVD configurations, CNA vendor+product blocks, EUVD products, KEV vendor/project | Only when joined to owned SBOM-to-image evidence, or promoted by a curated CPE-to-package map | `affected_derived` on an SBOM-to-image path. Otherwise `possibly_affected`. |
| C. CVE-keyed enrichment | CVSS, EPSS, KEV, SSVC, EUVD exploited flag, Exploit-DB references | Never | Changes priority only |
| D. Suppression and retraction | VEX, CNA `unaffected`, distro not-affected, withdrawn, REJECTED with `replacedBy`, reachability | Never. It can lower or retract. | n/a |

This matches the current reducer: `classifySupplyChainImpactProduct` returns
`affected_derived` only on an SBOM-to-image path, and the public rule is "no
finding from a product-only CPE alone". Each new source lands in one class.
EUVD and the CVE List add little to class A. Their value is in classes C and
D: severity for CVEs that NVD defers, SSVC, the EU KEV list, and REJECTED
state.

### 4.2 Advisory identity

The current identity, `FirstNonBlank(cve_id, advisory_id)`, has three gaps:

- GHSA-only advisories get no CVE-keyed enrichment. The 46 in QA have no CVE
  alias at all, so nothing could join them anyway.
- A CVE alias that arrives later creates a second finding, because `cve_id`
  is part of the finding id and the canonical key.
- Suppressions match by string only.

The proposal follows osv.dev:

1. **Edges.** Collectors emit raw identity edges on every advisory record:
   `aliases` (symmetric), `upstream` (directed, distro to CVE), `related`
   (display only), `replaced_by`, and status (`withdrawn`, `rejected`,
   `disputed`). This is a minor version of `vulnerability.cve`.
2. **Groups.** The reducer derives advisory groups over alias edges only.
   - It skips withdrawn records.
   - It quarantines, rather than silently drops, records with more than 5
     aliases and groups with more than 32 members.
   - It keeps both an allow list and a deny list, like osv.dev.
   - The group id is the lowest CVE id in the group, else the lowest
     advisory id.
   - A group with more than one CVE keeps each CVE for matching. A finding
     keyed by the group takes the highest EPSS of its member CVEs, and is
     KEV-flagged if any member CVE is in KEV. Its provenance records which
     member supplied each value.
   - `upstream` passes CVE enrichment down to distro records, never up.
3. **Keys.** Findings key on the group id. `canonical_key` and `finding_id`
   are built in two SQL sites that must stay byte-identical: the winners store
   and `findings_queries.go`. Both move together, with a parity test.
4. **Group changes.** A late alias merges two groups, and a withdrawal or a
   removed alias splits one. Each change re-keys the affected findings and
   carries operator suppressions across the change. Re-keying needs:
   - a `dependencyCatalog` edge so the producing domain re-triggers
     `supply_chain_impact` across scopes;
   - a readiness-floor entry.

Graph projection of the groups is a separate decision (Phase 3, item 3.6).
Findings stay in Postgres either way.

### 4.3 Enrichment refresh

**What the code does today.** KEV and EPSS are projector trigger kinds. Each
enrichment generation runs `supply_chain_impact` for its own scope, loads the
matching package and consumption evidence across scopes, and writes its own
copy of each matching finding. The winners table then keeps the copy with the
highest `priority_score` for each `canonical_key`. In QA, the KEV scope writes
0 findings because the intersection is empty, so this path cannot be observed
there.

**Hypotheses, unproven until a fixture test settles them:**

- Every enrichment scope that matches writes its own duplicate copy of a
  finding.
- A rescore that lowers EPSS is hidden by an older copy that kept a higher
  priority.
- EPSS targets hold up to 100 CVEs each. The 447 finding CVEs in QA today need
  about 5 target scopes, each writing its own copy set. The count grows with
  the number of findings.
- KEV re-runs about every 30 minutes (188 generations in 93 hours) whether or
  not the catalog changed.

**First step.** Write a fixture test of the current behavior. Then choose one
of:

- Keep per-scope copies, but fix the masking and add the content-digest hint
  from item 1.4 so unchanged generations are skipped.
- Read-time enrichment: CVSS, EPSS, KEV, and SSVC live in a reducer-owned
  table keyed by advisory group, and priority is computed at read time.
- CVE-keyed fan-out into the scopes that hold the findings.

**Theory proof for read-time enrichment.** `EXPLAIN ANALYZE` must cover:

- the canonical-winner sort;
- the `severity_bucket` filter and the aggregates;
- the ordered list queries, which use the
  `(severity_bucket, priority_score DESC, finding_id)` index;
- the 30-second winners resweep;
- the largest realistic finding count.

The arbiter reviews the choice.

### 4.4 Query model versus mirror

Eshu matches by querying for owned packages (OSV `querybatch` per
package@version), which keeps the fact volume tied to what Eshu owns. A
mirror of everything (414k CVE records, 2.56M OSV records) is a different
product surface: a browsable catalog, and matching that does not wait for a
derivation pass.

The generation model does not fit one large mirror scope:

- One claim writes one generation, and absent records retract.
- The successor guard is off.
- The source cache holds whole envelope slices in memory.

The proposal for Phase 5 is **shard-scoped generations**.

- Map each source to stable shards. For the CVE List, use its own
  `cves/<year>/<NNxxx>` directories. 414k records at up to 1,000 per
  directory is roughly 400 or more shards. For OSV, use ecosystem plus id
  prefix.
- A delta (CVE List `deltaLog.json`, OSV `modified_id.csv`) marks shards
  dirty. Only those shards are re-collected, in full, from the local
  baseline.
- Each generation stays full-replace for its shard, so retraction stays
  correct and the successor guard can be armed.

This is a theory. Item 5.1 measures it before anything is built.

Until then, Phases 1-3 stay query-driven, using clients that already exist:

- EPSS: the per-CVE JSON API with derived targets.
- NVD: a rolling modified window.

An EPSS daily-CSV client and any per-CVE CVE List fetch are new clients, so
they fall under the §4.7 ADR gate.

### 4.5 Cadence per source

cve.now chose the cadences quoted in the request, and some of them go against
upstream guidance. Proposed cadences:

| Source | Cadence | Mechanism |
| --- | --- | --- |
| CVE List v5 | 10-15 min | `deltaLog.json` cursor, git as fallback. The hourly zips are cumulative and irregular. |
| GHSA | 15-30 min | REST or GraphQL `updated` cursor, with a token. |
| OSV | 15 min for a mirror; derived rotation for matching | `modified_id.csv` cursor |
| NVD | 2 h | Rolling `lastModStartDate`, every page, API key, 6 s between requests (NIST guidance) |
| EUVD | 1-2 h | `/api/search` by `fromUpdatedDate`, overlapping one day |
| KEV | 1 h | Conditional GET; diff whole records |
| EPSS | daily, after publication | Published about 13:30Z; poll at 13:45Z for margin. Store `model_version`. |
| Exploit-DB | daily | GitLab repository CSV, never the website |

Cadence is set per instance today. Item 2.4 adds a per-target interval or one
instance per cadence class.

### 4.6 Licensing

Every source snapshot records its license and its attribution requirement:

- **NVD and CVE List.** The NVD notice and the MITRE CVE notice appear in the
  API and console footers.
- **GHSA.** CC-BY 4.0 attribution.
- **OSV.** Chainguard and Wolfi records are CC-BY-NC-ND. They are 62% of OSV
  records and are excluded from any mirror unless an operator opts in.
- **Exploit-DB.** Facts carry metadata only (EDB id, CVE codes, verified flag)
  from the GPL v2 repository, pending owner and legal sign-off.
- **EPSS.** The attribution text is not confirmed. Ask FIRST before shipping.

### 4.7 Where collector changes land

The arbiter ruled on 2026-10-26, and the epic issue records the ruling. It
was given four options: in-tree with a port, extracted-only, a split by size,
or another justified choice. It chose the split by size, with amendments:

- **The in-tree collector stays the only producer.** The extracted repository
  `eshu-collector-vulnerability-intelligence` holds no producer authority
  until the #8870 cutover. Nothing freezes in-tree changes.
- **Collector fixes land in-tree now, with a port in the same wave.** This
  covers items 1.0, 1.1, 1.3, 1.4, 2.2, and 3.1.
  - The extracted-repository PR is opened and cross-linked before the in-tree
    PR merges.
  - Both mirror tests gain the same assertion.
  - Each change adds an input-to-payload golden that both trees load from
    `sdk/go/factschema/fixturepack/payloads/`.
  - The extracted repository moves to the current SDK release candidates
    first.
- **Back-port the fragment redaction first.** #9035 was fixed only in the
  extracted repository (PR #2 there). The in-tree `sanitizeURL` still keeps
  URL fragments, so parity is broken in the unsafe direction.
- **New sources and mirror mode need an ADR, then land only in the extracted
  repository after cutover.** The ADR runs in parallel with the cutover. If
  the cutover is still blocked when the ADR lands, escalate the blockers
  rather than build in both trees.
- **The epic is a new top-level epic** that carries `Refs #8870.` and
  `Refs #6707.`

Core-side work stays in `eshu` regardless: the reducer, factschema contracts,
query, readiness, coordinator derivation, MCP, and the console.

### 4.8 Rules this plan touches

- **Core behavior invariant.** `go/internal/reducer/supplychain/core/AGENTS.md`
  says every change is behavior-preserving. That rule dates from the package
  split. Phases 1-3 change behavior there on purpose, so the first core PR
  rewords the rule: package moves preserve behavior, and behavior changes
  need a regression first. The arbiter reviews the wording.
- **Collector corpus rules.** The scoped collector `AGENTS.md` forbids
  crawling a full public corpus and turning public records into graph writes.
  The EPSS daily CSV, the CVE List and OSV mirrors, and any graph projection
  that the collector would write all fall under the ADR. The 3.6 projection
  would be reducer-owned, not collector-owned.

## 5. Measures of done

All measured in the shared QA environment through MCP and read-only SQL.

| Measure | Now | Target |
| --- | --- | --- |
| Winners with severity `none` whose source has a label or vector | 506 of 523 OSV-scope | 0 |
| Ecosystems with derived OSV targets | 1 | every ecosystem present in owned evidence |
| CVE-keyed findings with EPSS | 0 of 945 (first read) | at least 95% |
| KEV-flagged findings vs findings whose CVE or alias is in KEV | 0 vs 0 (no overlap yet) | equal. The QA check passes trivially until overlap exists, so item 2.3 adds a seeded KEV CVE fixture as the real proof. |
| Configured sources with fresh readiness state | `unknown` | every configured source |
| Time from upstream publish to finding | unmeasured | measured per source, then a budget |

## 6. Epic and issues

The phases file lists one child issue per row of its tables. Issue bodies
follow the `eshu-publish` templates and pass `check-shape.sh`. Each child
copies its item's proof and QA check.

## 7. Open decisions

| Decision | Owner |
| --- | --- |
| Where collector changes land (§4.7) | Arbiter: decided 2026-10-26 |
| New-source and mirror ADR (Phases 4-5) | ADR, then arbiter |
| Enrichment refresh model (§4.3) | Fixture test and theory proof, then arbiter |
| Wording of the core behavior rule (§4.8) | Arbiter |
| Graph projection of advisory groups (item 3.6) | Arbiter |
| Mirror and catalog scope (Phase 5) | Owner |
| Unreviewed GHSA records | Owner |
| Exploit-DB and EPSS licensing | Owner and legal |
| Opting in to NC-ND OSV ecosystems | Owner |

No-Regression Evidence: this plan changes no runtime behavior, wire contract,
or gate. Each phase item carries its own proof requirement.

No-Observability-Change: this plan adds no metric, span, log key, or status
field. Runtime items name their operator signal in the phases file.
