# #9880 closure proof — deployed QA evidence (2026-11-10)

Ruling: `docs/internal/arbiter/9880-closure-dispositions-ruling-2026-11-10.md`.
This record covers Decision 4 items 1–8 against deployed QA after the
#10524 minting fix shipped (core `sha-4a68cfee49`, synced ~22:40Z).

Proof session: 2026-11-10 ~22:49–23:30Z. All reads below are from that
session unless timestamped otherwise.

## 1. Contemporaneous external re-reads (2026-11-10T22:49:15Z)

| CVE | CVE Services (CNA container) | NVD `vulnStatus` + metric types |
|---|---|---|
| CVE-2026-96780 (primary) | `cvssV4_0` 8.2 HIGH, CNA `GitHub_M` | Deferred, Secondary only (no Primary score) |
| CVE-2026-97045 | PUBLISHED, CNA container has title/descriptions but **no metrics** | Deferred, Secondary 3.1 6.1 MEDIUM |
| CVE-2026-97711 | `cvssV4_0` 2.3 LOW | Awaiting Analysis, Secondary |
| CVE-2026-94482 | `cvssV4_0` 8.2 HIGH | Awaiting Analysis (GHSA-sourced record) |

Primary premise holds: 96780 is NVD-deferred with no Primary score and
CNA-scored 8.2 HIGH.

## 2. Owned-package trace (deployed system)

`list_advisory_evidence` (`cve_id: CVE-2026-96780`) returns
`affected_packages` with `purl: pkg:npm/figlet`, fixed in 1.11.3, sourced
from osv facts — the QA finding ties the primary CVE to an owned npm
package in the deployed system, not quoted from the overlay comment.

## 3. Facts land (previous all-time zero replaced)

`fact_records` for `vuln-intel://cve_list/cve` at 23:0xZ:

- `vulnerability.cve`: 4 (one per owned ID)
- `vulnerability.reference`: 13
- `vulnerability.source_snapshot`: 1 (MITRE-attributed snapshot)
- `workflow_work_items` for the scope: 1 claimed
- `vulnerability_source_states`: `fresh`, `succeeded`, `result_count: 18`,
  `last_success_at: 2026-11-10 22:50:03Z`

## 4. Query surface shows CNA severity with `cve_list` provenance

`list_advisory_evidence` for CVE-2026-96780 returns truth `exact`, freshness
`fresh`, with a `sources` entry: `source: cve_list`, `cvss_score: 8.2`,
`severity_label: HIGH`, `cvss_metrics.cna_cvss_v40: 8.2 HIGH`. The score is
attributed to the `cve_list` path, not the NVD Secondary mirror.

## 5. Freshness fresh

Source-state row for the scope reports `fresh` (see item 3 timestamps).

## 6. Supporting members (no silent omissions)

| CVE | Shown severity / provenance | vs item-1 re-read |
|---|---|---|
| CVE-2026-97045 | 6.1, `adp_cvss_v31` MEDIUM, `source: cve_list` (no severity label; CNA container carries no score) | No contradiction: CNA has no score; NVD Secondary 6.1 MEDIUM mirrors the ADP assessment |
| CVE-2026-97711 | 2.3 LOW, `cna_cvss_v40`, `source: cve_list` | Matches CNA 2.3 LOW |
| CVE-2026-94482 | 8.2 HIGH, `cna_cvss_v40`, `source: cve_list` | Matches CNA 8.2 HIGH |

## 7. Authorized scan-impact health (Decision 3)

After `cve_list` facts landed, supply-chain scan-impact derivations in QA
succeed (sampled `supply_chain_impact` reducer executions 22:50Z, all
`succeeded`). Zero occurrences of `unsupported`, `validateAuthorizedScan`,
or `not supported` across 90 minutes of reducer + coordinator logs. No
unsupported-source-class failure attributable to a `cve_list` authority
observed — Decision 3 branch 2 (healthy-shown): the residual code gap
(allow-list omission, re-verified present on current main) is filed as
#10546, named in the closure comment, with end-to-end scan admission
expressly not claimed.

## 8. Fetch accounting

One `cve_list` claim covering all 4 IDs, `result_count: 18`, succeeding at
22:50:03Z in the first post-deploy plan-key bucket. No duplicates, no
retries observed for the scope; volume is one fetch set per bucket per the
#10519 deferral (pacing above 1 req/s not separately instrumented — see
NOT_CHECKED).

## Deferred / not claimed

- #10519 cadence deferral stands; per-bucket re-fetch is the known interim.
- Fetch pacing instrumentation (1 req/s) was not separately measured.
- GHSA rolling-window execution in QA fails terminal (token gap, tracked as
  #10545) — independent of this closure; no GHSA claim made here.
- No performance claim; no legal clearance beyond the owner's 2026-11-05
  decision (counsel items open/non-blocking, named in the ruling).

NOT_CHECKED: per-claim pacing (1 req/s) — counts show one 4-request fetch
set in the bucket with no duplicates, retries, or over-budget volume, but
inter-request pacing was not instrumented, so Decision 4 item 8 is satisfied
on counts and bounded volume, not on measured pacing. Everything else in
items 1–8 is evidenced from the deployed system in this session.
