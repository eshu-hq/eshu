# 11087: Provenance Bound On Transitive Product-Criteria Expansion

Refs #11087 (part of #10860; evidence on #10972). Arbiter-ruled before any
semantic change; this record is acceptance criterion 1.

## Question

The supply-chain impact evidence load walks shared product-criteria strings
transitively: a vulnerable affected-product row adds its criteria and CVE id
to the next round, which loads every row sharing the criteria string, whose
CVE ids load their rows in turn. Is that transitive (hop >= 2) expansion
required for finding correctness? If not, what bound keeps every finding?

Measured on QA at deployed commit `664cd7533d` (#10972 comments of
2026-11-21): runs stopping at the 8-round limit loaded about 46,000
affected-product rows, completing runs 2; 437 of 6,500 scopes sat partial
(402 rounds, 35 evidence budget), longest 251 hours. The wildcard criteria
`cpe:2.3:o:linux:linux_kernel:*:...` alone is named by 2,743 CVEs.

## Derivation Evidence (source, base `fc5593bcc9`)

A loaded affected-product row can affect output only through this chain:

1. Indexed only if vulnerable, not negated, criteria non-empty, keyed by CVE
   (`index_build.go:149-164`).
2. Classified only for CVE groups present in loaded CVE records, and only
   when the CVE has no native affected packages (`impact_classify.go:22-41`).
3. Anchored only by exact-string component match (`purl_match.go:34-39`,
   `EqualFold` criteria == component CPE; no version range is ever
   evaluated) plus attachment digest plus usable image (`match.go:413-438`,
   `product.go:18-41`).
4. Otherwise the weak product finding has no repository or digest and is
   dropped by the owned-anchor gate (`findings_build.go:97-112,128-133`);
   runtime context cannot rescue it without a digest first
   (`runtime.go:145-152`).

Therefore the only output-relevant criteria hops are **A** (a loaded
component's CPE pulls matching product rows) and **B** (a seed CVE's criteria
pull matching components). Hop >= 2 adds nothing: if a transitive row could
anchor, the anchoring component is loaded and its CPE is component-origin,
so hop A loads that row directly, and the CVE record then loads via the CVE
arm. Empirically, the chain fixture's unbounded unanchored findings are
empty (arbiter run, and `product_test.go` product-only case).

Sole residual: alias-group composition via an asymmetric alias (transitive
CVE T lists the seed, the seed does not list T; union at
`os_advisory_alias_group.go:307-309`). Accepted and shim-pinned: today's
truncated passes already disable grouping entirely
(`findings_build.go:66-69`), so a bounded complete pass is strictly better
than the status quo for the 437 partial scopes.

## Chosen Bound (provenance)

- Seeds = the pass's initial-filter CVE ids plus, for a scoped pass, the
  stamp set, closed only over alias and upstream identity of seeded CVE
  records as they load.
- Component-origin criteria (borne by an **owned** SBOM component) drive both
  the `affected_product_criteria` and `sbom_component_cpe` probes. A component
  is owned when its scope is the pass's own intent scope, or its document is
  attached (any scope) to a subject digest that an envelope of the pass's own
  scope carries. Suppression rows never root a document: their scope map can
  name any digest. A component reached through a document rooted in another
  source's digest is foreign: its criteria are suppressed (counted) and that
  source's own run follows them. The CVE arm is untouched, so enrichment and
  severity refresh stay seed-based.
- Seed-CVE product-origin criteria drive only `sbom_component_cpe`
  (`SeedProductCriteria` on the filter).
- All other derived criteria are suppressed at derivation and never
  requested; each round's suppressed distinct count is telemetry.
- The unconditional CVE-arm widening for non-vulnerable rows
  (`active_filter.go` derivation) **stays**: non-vulnerable rows never emit
  criteria, the CVE probe cannot reach a new CVE by itself, and the widening
  is required for record, severity, retraction, and alias completeness of an
  already-reached CVE.

Storage binding: the shared criteria parameter carries the union; the
resolve loop rebinds `affected_product_criteria` **and** `foreign_key_guard`
to the component-origin list when `SeedProductCriteria` is non-empty.
Full-filter branches keep the union after audit: suppression, platform,
workload, and service payloads carry no top-level criteria or CPE keys, so
the union cannot change their row set; the guard covers affected_product
(kind class growth), so its criteria arm would otherwise re-admit exactly
the rows the bound excludes — expected-empty via omitted keys is not proof
by construction, hence the rebind. Seam off (no seed list) the union equals
`ProductCriteria` and every binding is byte-identical to before.

## Rejected Alternatives

- **Wildcard/versionless-only block**: versioned chains and versioned
  super-connectors still blow the 8-round cap, and a component may
  legitimately bear a wildcard CPE (hop A must stay).
- **Version-range-aware matching**: infeasible on current facts.
  `AffectedProduct` carries no version bounds
  (`supplychainmodel/types.go:135-162`) and classification is exact-string,
  so load-side range filtering would diverge load from classify. Needs
  collector/schema work; follow-up spec returned to the coordinator.
- **CVE-count fan-out cap**: data-dependent semantics; re-creates partial
  scopes or silent loss at the cap. Accuracy violation.
- **Ecosystem-scoped expansion**: product rows have no ecosystem field and
  NVD-window scopes are ecosystemless.
- **Stop the criteria-to-CVE hop entirely**: kills hop A and loses anchored
  derived-product findings for SBOM-only scopes.
- **Raise the round cap or evidence budget**: masks the defect; forbidden.

No claim, lease, ordering, worker-count, or batch-size change. The bound
touches only follow-up filter derivation and two probe bindings.

## Parity Shim (acceptance criterion 2)

`criteria_expansion_parity_test.go`: oracle = `BuildSupplyChainImpactFindings`
over the full synthetic universe envelopes (complete-evidence truth, no
loader); bounded = until-stable load through a production-shaped stub (the
CVE probe returns CVE, package, product, and signal rows, matching
`branches.go` vulnerability kinds), then Build over the loaded set. Full
finding keys are compared (CVE, advisory, criteria, status, severity,
digest, repository, evidence fact ids, group members). Fixtures: 9-hop
versioned chain plus a wildcard super-connector (50+ CVEs) with an anchored
component at a hop >= 2 criteria; hop B; a case-variant pair (non-canonical
but consistent spelling on both sides of the join; cross-spelling
classification is proved at the split level by
`TestCriteriaExpansionBoundMatchesClassifyNormalization`, since storage binds
both arms by exact match); asymmetric alias (only allowed
delta: group members, asserted explicitly); non-vulnerable row; native
packages suppressing products; scoped variant. Truncation conversion on the
chain fixture: unbounded is partial at the round cap, bounded is complete.

The golden cassette harness is classify-only (it replays envelopes into
Build, never the loader), so the loader seam cannot affect it; the synthetic
QA-shaped differential above is the AC2 evidence. Measured counts are
recorded in the PR from the test log.

## Telemetry

Every pass emits result sub-signals, seam-off zeros included for
comparability: `criteria_bound_suppressed_transitive`,
`criteria_bound_loads`, `criteria_bound_component_criteria`,
`criteria_bound_seed_criteria`, `criteria_bound_seed_cves`. Success is
judged on loads/rounds plus pass-state conversion, never on suppressed > 0
(pure-seed scopes correctly read 0). Keys documented in
[supply-chain active evidence telemetry](../../public/reference/telemetry/supply-chain-active-evidence.md).

## Rollback And Post-Deploy Proof

Rollback is one line: remove `CriteriaExpansionBound` from the production
handler wiring in `defaults_additive_domains_supply_chain.go`; nil keeps the
loop on the prior semantics and the storage union degenerates to today's
binding.

Post-deploy (coordinator, acceptance criteria 3-4): rerun the
`supply_chain_impact_scope_pass_state` count and age-by-cause queries from
#10972 (2026-11-21T01:08:19Z) — partial count and age must fall because
truncated runs convert to complete — and the vuln-filtered drain reading
from #10972, which must not regress.

## Evidence Markers

Performance Evidence: baseline is the unbounded until-stable loop on the
synthetic QA-shaped universe in `criteria_expansion_parity_test.go` (Go test
harness, stub loader that mirrors the `branches.go` vulnerability kinds, no
database). Input shape: a 9-hop versioned criteria chain plus a wildcard
super-connector of 50+ CVEs, one anchored component at a hop >= 2 criteria.
Measured by `TestCriteriaExpansionBoundParityAcrossFinalFindings`: the full
universe is 60 envelopes; the unbounded load pulls 38 of them, the bounded
load 26, with 4 distinct transitive criteria suppressed, and final findings
equal (5 oracle, 5 unbounded, 5 bounded, full-key equality with the oracle).
`TestCriteriaExpansionBoundSuppressesTransitiveChain`: on the chain-only
fixture the unbounded loop stops at the round cap (`truncation.rounds`,
partial), while the bounded loop converges in 1 request with no partial
cause. This is test-harness evidence of the mechanism; the QA figure (46,000
rows at the cap, 437 of 6,500 partial scopes at `664cd7533d`) is the baseline
the post-deploy reading below must move. No SQL text, index, or predicate
changes; the storage change rebinds two existing probes to a shorter list, so
each follow-up round issues the same statements over at most as many keys.

No-Regression Evidence: the seam-off path is byte-identical
(`TestCriteriaExpansionBoundSeamOffIsNoop`, and the storage test asserts every
branch keeps the shared bind map when `SeedProductCriteria` is empty). No
claim, lease, queue, ordering, worker-count, or batch-size change.

Observability Evidence: five result sub-signals ride every pass (listed under
Telemetry above and in the public active-evidence telemetry page);
`supply_chain_impact_scope_pass_state` already reports the partial markers
that must fall after deploy.

## Arbiter Follow-Up: Shared Documents, Arrival Order, Rounds

Ruling 2026-11-21 on the #10972 two-part-rule comment: ship the bound for the
product path; "own data only" is not adopted wholesale (it would drop the
enrichment and severity refresh edge); the ownership test stays seed-based;
the package path is #11097 and version-range matching is #11098.

The one place the first cut followed another source's data was a shared SBOM
document: batch B reaches an SBOM scope's component through its seed's
criteria, the document probe loads that document's other components, and a
component bearing a wildcard CPE then counted as component-origin and pulled
every CVE sharing it. `criteria_expansion_shared_document_test.go` pins it.
Without the restriction below, `bounded B loaded kernel row "cve-k1" outside
its seed set` (loaded set: `prod-k1..3`, `cve-k1..3`, `epss-k1`). The fix:
component-origin criteria first came only from components of the pass's own
intent scope (`ownScopeID`, passed from `intent.ScopeID`), and the review P1
below then added the own-digest rule. The foreign component's criteria are
suppressed and counted; the CVE arm is untouched, so a seed CVE's signals,
severity, and aliases still load.

Evidence (go test, harness, no database):

- `TestCriteriaExpansionBoundSharedDocumentDoesNotFollowForeignCriteria`:
  bounded B loads 4 rounds and 3 suppressed criteria, loads no kernel row,
  emits exactly its own finding; the unbounded control loads 5 rounds and
  follows the wildcard into every kernel CVE.
- `TestCriteriaExpansionBoundArrivalOrderFleetParity` (vuln-first, SBOM-first,
  all-at-once): the union of each arriving source's own run equals the
  full-universe oracle on content (score, severity, EPSS, KEV, priority,
  exploited tier, anchor, evidence ids), 4 findings each. Vuln-first relies on
  the SBOM scope's own run (hop A over its own components) for the kernel
  findings; SBOM-first relies on each batch's own run (hop B over its own
  seeds). The pass count is the existing one: a source runs when its scope
  arrives.
- `TestCriteriaExpansionBoundEnrichmentChangeContent`: after an EPSS change on
  a kernel CVE and a KEV listing on the seed, each own run emits the new
  content (equal to the new oracle). The SBOM scope's earlier copy is
  identical with and without the bound, so the bound adds no staleness; its
  re-run equals the new oracle. A copy held by a source that does not re-run
  stays stale under both rules; that edge is pre-existing and not changed here.
- Rounds: every bounded fixture asserts `loads <= 8` and no round-cap
  truncation (`assertRoundsWithinCap`); measured 1 to 4 loads across these
  fixtures. The 7-round reading in the throwaway #10972 shim is not
  reproducible here: it used the shim's union-list loader with package-path
  switches, a different loop. In this loop rounds are the dependency depth
  (seed, component, document and attachment, digest and image), at most 4 on
  these fixtures, not the fan-out.

Why the synthetic shim is the product-path proof: QA has no product-name
findings (0 in the 300-run sample; 2 components carry a product name), so a
QA before/after cannot show finding parity on this path. The harness above,
plus the live golden-corpus gate in CI, is the evidence; the post-deploy QA
reading shows load and partial-scope movement only.

## Review P1: Image-Last Arrival And The Own-Digest Rule

An independent review found that the scope-only ownership test dropped every
component for an image or OCI pass: such a pass reaches its SBOM only through
the attestation attachment, so every component is foreign by scope, and an
image arriving last emitted none of the findings its own digest anchors (fleet
0 of 4 against the oracle in the review probe). The arbiter ruled option (b):
documents rooted in the pass's own digests count as own (rule in Chosen
Bound). Rejected: keeping the scope-only test and relying on fan-out (late
attachments are not covered), a scope-kind test, and reverting the restriction
(it weakens the load bound).

Arrival edges, with their carrier:

- Container image identity and CI-run correlation arrival re-run every
  `supply_chain_impact` consumer: `go/internal/reducer/crossscope/dependencies.go`
  lists both as producer domains of `DomainSupplyChainImpact`, and
  `go/internal/storage/postgres/scope/completion/fanout.go` fans the
  completion out to every active consumer scope.
- Attestation attachment arrival has no fan-out (the attachment is written
  under the intent's own scope and is not a producer in the catalog). It is
  carried by the own-digest rule: the image scope's own run owns the document
  its digest attaches. A follow-up issue tracks whether a re-run edge is
  missing.

The lean per-envelope digest helper (`ownershipDigests`) reads the digest keys
of `supplyChainImpactFilter` without the suppression case;
`TestCriteriaBoundOwnershipDigestsMatchFilter` pins the two together.

Fixtures (`criteria_expansion_image_arrival_test.go`, image scope an arriving
source; each runs bound on and off, compares finding content with the
oracle, 4 findings, and never stops on a truncation cause). RED at
`db3f666c9f` (scope-only rule): a, b, c (bounded arms) and the enrichment
fixture with the image scope as a running source fail; the own-digest rule
turns them green. Bounded loads per fixture (max over the runs in the
fixture, cap 8):

| Fixture | Bounded loads (max) |
| --- | --- |
| a image-last, attachment in SBOM scope, only the image scope runs | 5 |
| b image-last, attachment in image scope | 4 |
| c late attachment in image scope, only the image scope re-runs | 4 |
| d image-last with the catalog fan-out re-running every consumer | 4 |
| e vuln-last: SBOM, then image plus attachment, then the batches | 5 |
| f batch B with the attachment in the image scope loads no kernel row | 4 (suppressed 3) |
| g suppression naming the digest does not root the document | 2 (suppressed 1) |
| h repository scope last, through the image identity row | 5 |
| i EPSS/KEV change with the image scope running | 5 |

Fixture h states who carries the finding: the image scope's own run carries it
under the own-digest rule, no fan-out needed. The repository scope's own run,
which reaches the digest through a foreign image identity row, emits 0 findings
bounded (3 loads) against 4 unbounded (6 loads); the repository-anchored
findings exist in the fleet through the image run. Ownership is not widened
beyond (b).
