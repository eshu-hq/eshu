# Closure dispositions for #9880 — arbiter ruling (2026-11-10)

> Landing note (coordinator, not arbiter text): recorded 2026-11-10 after
> the ruling. Since the ruling, #10524 merged (`b0df4f55`, which also
> resolves the ruling's "#10524 is OPEN" premise) and the reducer
> scan-gate omission in Decision 3 was re-verified still present on
> current main. Deployment of the minting fix (core repin live) is still pending at
> landing time, so Decision 4's precondition still gates the proof.

RULING: #9880 closes only on the deployed QA proof in Decision 4; the
four-CVE owned list is accepted with CVE-2026-96780 as sole primary
(re-verified at proof time); the #10244 "Minor" contract classification is
accepted for schema versioning only, with the reducer scan-gate P1 carved
out; fetch counts are judged per plan-key bucket, never against
`refresh_interval: 1h`, while #10519 is deferred.

Arbiter: `arbiter-eshu` (`muse-wonder-full-1.0`). Skills loaded:
`eshu-code-review`, `eshu-contract-rigor`. This ruling is fresh from the
sources below; a prior-session summary of an earlier ruling was reported
lost and is given no credit.

## Facts the ruling rests on

- #9880 is OPEN. Its acceptance criteria are met in code except two items:
  the QA-after-deploy check ("a CVE that NVD defers shows severity from the
  CNA") and, in deployed form, the freshness operator signal. The remaining
  criteria (fixtures for CNA/ADP metrics, SSVC, REJECTED with `replacedBy`;
  envelope with source-namespaced key, source enum, reducer rank, readiness
  source map, registry overrides, license-carrying snapshot; contract
  classification with factschema-diff; B-7 cassette and snapshot) were
  delivered by core #10244 (merged, merge commit `70ec8589`) and satellite
  `eshu-collector-vulnerability-intelligence` #57 (merged by owner order,
  merge commit `328c4349`). Satellite follow-ups #59, #60, and #61 are
  CLOSED (arbiter-verified via `gh issue view`).
- Owner decision recorded 2026-11-05 on #9880: collection approved, scoped
  to owned CVEs, with the MITRE attribution and Terms of Use link carried in
  the snapshot and UI, a budget of at most one request per second, and at
  most 100 CVE IDs per claim. Counsel questions remain open and
  non-blocking; the decision is the owner's, not a counsel opinion.
- The deployment overlay (the QA `app-values.yaml` in the deployment
  repository) carries a `source: cve_list` target with
  scope `vuln-intel://cve_list/cve`, an explicit `cve_ids` list of
  CVE-2026-96780, CVE-2026-97045, CVE-2026-97711, and CVE-2026-94482, and an
  evidence comment stating the four IDs sit on owned-package findings in
  that environment and were NVD-deferred or awaiting analysis with a
  CNA-published score when read on 2026-11-10, with CVE-2026-96780 named as
  the acceptance example (NVD Deferred, CNA 8.2 HIGH). The comment also
  states the list grows only with the same owned-package trace.
- Arbiter re-read on 2026-11-10 (public CVE Services and NVD APIs):
  CVE-2026-96780 is PUBLISHED with CNA metrics `cvssV4_0` baseScore 8.2,
  baseSeverity HIGH (CNA `GitHub_M`), plus a CISA-ADP container; NVD shows
  `vulnStatus` Deferred and carries only a Secondary (CNA-sourced)
  `cvssMetricV40`, i.e. no Primary NVD analyst score. The acceptance premise
  holds today.
- #10244's body classifies the contract change as Contract System v1
  "Minor": additive optional payload keys (`ssvc`, `unaffected`,
  `cvss_metrics` detail on `vulnerability.cve`, whose schema allows
  additional properties with only `advisory_id` required), one new
  `cve_list` source value, no new fact kinds, and no removed, renamed,
  narrowed, or newly required fields; `scripts/verify-factschema-diff.sh`
  exited 0, and the merge report on #9880 records `contracttest`,
  `factschema-diff`, payload-usage-manifest, and fact-kind-registry green
  on the merged head.
- Two review bodies on #10244 raised one P1 twice: storage admits
  `cve_list` authorities via `authorizedImpactExpectedKinds` (in
  `go/internal/storage/postgres/supply_chain_impact_authorized_advisory.go`),
  while the reducer's `supportedTrustedAdvisorySourceClass` (in
  `go/internal/reducer/supplychain/core/derived_partition.go`) omits
  `cve_list`, so `validateAuthorizedScanImpactEvidence` fails closed on any
  scan target once a `cve_list` authority exists. Arbiter verified the
  omission still holds on `origin/main`: the case list has no `cve_list`.
  The merge report dispositioned only the P2 coverage-eligibility guard
  (fixed with mutant RED/GREEN proof); the P1 has no recorded disposition.
- #10524 (activation static-target minting) is OPEN at ruling time. It
  records that QA previously showed zero `fact_records`,
  `workflow_work_items`, and `vulnerability_source_states` rows for
  `vuln-intel://cve_list%` because `PlanActivationDerivedWork` never minted
  configured standing targets; its own NOT_CHECKED defers deployed QA proof
  to this #9880 closure drive. Its deferral of #10519 (`refresh_interval`
  ignored on the activation path; severity-table category "minor
  performance") was accepted under the #10519 cadence-deferral ruling
  recorded in that PR, with cadence parity expressly not claimed.

## Decisions

### 1. (a) Owned list — ACCEPTED as the closure set, 96780 sole primary

The four-ID list is an acceptable closure set, on these terms:

- CVE-2026-96780 is the sole primary acceptance example. It alone must
  satisfy, at proof time: present on an owned-package finding in the QA
  environment, NVD-deferred (no Primary NVD score), and CNA-scored. The
  arbiter's 2026-11-10 re-read confirms the last two today (Facts above);
  the owned-package trace is asserted by the overlay evidence comment and
  must be re-shown in the closure proof, not inherited from the comment.
- CVE-2026-97045, CVE-2026-97711, and CVE-2026-94482 are supporting members
  only. A supporting member that has since gained a Primary NVD score does
  not fail closure; its truthful scored behavior is recorded (Decision 4,
  item 6). A supporting member that errors, or whose shown severity
  contradicts its contemporaneous CNA re-read, does fail closure.
- If CVE-2026-96780 no longer qualifies at proof time (for example NVD has
  scored it), the primary example is void. The coordinator must then either
  promote a supporting member that qualifies on contemporaneous evidence
  and amend the overlay comment, or add a new qualifying owned CVE under
  the same owned-package-trace rule. Closure on the stale comment alone is
  refused.
- The list stays explicit IDs only, no window, well under the 100-per-claim
  cap. The overlay rule "grow it only with the same owned-package trace" is
  ratified and stays in force after closure.

### 2. (b) #10244 "Minor" classification — ACCEPTED for schema versioning; not a reader-tolerance proof

Under `eshu-contract-rigor`, Minor is an additive optional field change;
adding a member to a closed enum is Minor only where every reader keeps an
unknown member rather than failing the read. Judged against that rule:

- Accepted: no major bump and no conversion shim is owed. The payload
  additions are optional keys on a schema that allows additional
  properties, no field was removed, renamed, narrowed, or newly required,
  and the authoritative gate (`scripts/verify-factschema-diff.sh`, exit 0,
  plus payload-usage-manifest, fact-kind-registry, and contracttest green
  on the merged head) passed. Re-opening the classification would change no
  shipped schema and no consumer code; it is not a closure blocker.
- Carved out: the PR body's supporting claim that every closed reader was
  updated in the change was false at merge. One closed reader,
  `supportedTrustedAdvisorySourceClass`, still rejects `cve_list` (Facts
  above). "Minor" is therefore accepted as the versioning classification
  and is expressly NOT accepted as evidence that every reader tolerates
  the new source value. Decision 3 gates that remainder.

### 3. Reducer scan-gate P1 — closure-gated by QA observation

The undispositioned P1 from #10244's reviews is in scope for this closure
because it sits on the admission path #9880 created, and the closure drive
already owns code-gap fixes the sweep finds in the `cve_list` admission
area. Exactly one of these must hold at closure:

- Fixed: `cve_list` is added to `supportedTrustedAdvisorySourceClass` with
  a RED/GREEN regression through `validateAuthorizedScanImpactEvidence`
  (an `AdvisoryAuthorityDependency` with trusted source class `cve_list`
  fails before, passes after), landed before the closure claim; or
- Shown unreachable in the deployed system: closure proof item 7 (Decision
  4) shows authorized scan-impact derivation healthy after `cve_list`
  facts land, and the residual core gap is filed as a linked follow-up
  issue named in the #9880 closure comment, with authorized scan-impact
  admission expressly not claimed (Decision 5).

Silence on this P1 at closure is refused. The failure mode is fail-closed
and loud (no wrong graph truth), which is why observation-then-fix is
sufficient; had it silently dropped or misattributed data, the fix would
be unconditional.

### 4. (c) Closure-proof checklist — all items required, in this order

Precondition: #10524 merged and deployed (core repin live) before any item below is judged; proof captured before the minting
fix is void, since QA previously minted zero `cve_list` work.

1. Contemporaneous external re-reads, captured in the same proof session
   (timestamps recorded): CVE Services and NVD records for CVE-2026-96780
   showing a CNA-published score and NVD-deferred status with no Primary
   NVD score (as the arbiter observed on 2026-11-10: CNA `cvssV4_0` 8.2
   HIGH; NVD `vulnStatus` Deferred, only a Secondary CNA-sourced metric).
   Re-reads for the three supporting CVEs recorded alongside.
2. Owned-package trace: the QA finding(s) tying CVE-2026-96780 to an owned
   package in that environment, shown from the deployed system, not quoted
   from the overlay comment.
3. Facts land: after the deployed minting fix, `workflow_work_items` for
   `vuln-intel://cve_list/cve` are claimed and `fact_records` of kind
   `vulnerability.cve` (and the license-carrying `source_snapshot` with the
   MITRE attribution) exist for the primary CVE, replacing the previous
   all-time zero.
4. Query surface truth: the deployed query/API surface shows
   CVE-2026-96780 with severity equal to the contemporaneous CNA re-read
   (8.2 HIGH at ruling time) and provenance naming `cve_list` as the
   severity decider (NVD retained as alternate where the surface shows
   alternates). A matching score whose provenance is the NVD Secondary
   metric does NOT satisfy this item: the proof must attribute the score
   to the `cve_list` path, because NVD already mirrors the CNA score as a
   Secondary metric.
5. Freshness fresh: the `cve_list` freshness/operator signal for the
   scope reports fresh after the run (source-state no longer absent), with
   the observed state and timestamp recorded.
6. Supporting members: for CVE-2026-97045, CVE-2026-97711, and
   CVE-2026-94482, record each outcome against its item-1 re-read (fact
   landed, shown severity, provenance), including any that NVD has since
   scored; no silent omissions.
7. Authorized scan-impact health (Decision 3): after `cve_list` facts
   land, authorized scan-impact derivation in the deployed system shows no
   unsupported-source-class failure attributable to a `cve_list`
   authority; any such failure routes to the Decision 3 fix before
   closure.
8. Fetch-count accounting per Decision 5's per-bucket expectation,
   including the one-request-per-second pacing and the ≤100-per-claim
   budget observed for the proof runs.
9. Closure comment on #9880 listing items 1–8 with their evidence,
   the #10244 Minor disposition (Decision 2) quoted, the Decision 3
   outcome (fixed, or follow-up issue number and carve-out), and the
   standing #10519 deferral. No issue-closing keyword relies on this
   ruling alone; the comment is the closure record.

### 5. Fetch counts under the #10519 deferral — per-bucket, never 1h

- Expectation: at most one fetch set per plan-key bucket — with four owned
  IDs, at most four CVE Services requests per bucket — because activation
  standing statics re-mint per plan-key bucket through deterministic IDs
  plus open-target admission until #10519 lands. The proof states the
  bucket cadence it observed and judges counts against it.
- Fetch volume above one set per hour is not a closure failure while
  #10519 stands deferred; the overlay's `refresh_interval: 1h` is
  knowingly not honored on this path. Duplicates within one bucket,
  unbounded retries, pacing above one request per second, or more than
  four requests per bucket for this list are failures.
- Truth is never excused by cadence: a wrong severity is a failure at any
  fetch rate. #10519 remains the tracked owner of due / not-due / blank /
  bootstrap cadence proof; this ruling does not discharge it.

### 6. (d) What must NOT be claimed at closure

- No `refresh_interval` or 1h-cadence parity with in-tree targets
  (#10519 deferred), and no claim that observed per-bucket fetching is the
  intended permanent cadence.
- No coverage eligibility: `cve_list` is a readiness candidate only, never
  coverage-eligible and never fetch-confirmed coverage.
- No whole-corpus or windowed CVE List mirroring; owned explicit IDs only
  (whole-corpus is Phase 5, out of scope here).
- No claim that "Minor" proved every closed reader tolerates `cve_list`,
  and — unless Decision 3's fix landed — no claim of authorized
  scan-impact admission end-to-end.
- No legal clearance beyond the owner's 2026-11-05 decision: counsel
  questions on normalized-fact copying, CNA/CISA-ADP content reach, and
  trademark presentation remain open and non-blocking, and must be named
  as such, not resolved.
- No claim that satellite #57 merged on a clean review; it merged by owner
  order past blocking P2s, with follow-ups #59, #60, and #61 since closed.
- No severity claim attributed to NVD for the primary example, and no
  evergreen status for the four-ID list: it is evidence as of its proof
  date, extendable only under Decision 1's trace rule.
- No performance claim from this closure (no before/after measurement is
  part of the QA proof; the enqueue and fetch costs are bounded and
  stated, not measured wins).

## What the arbiter will not do, and why

- Will not re-classify #10244 as Major or demand a shim: the schema gates
  passed on the merged head and no consumer-visible break follows from the
  additive keys; the one intolerant reader is handled as a code gap
  (Decision 3), not a versioning defect.
- Will not accept closure on the overlay comment, the arbiter's own
  2026-11-10 re-read, or unit/fixture proof: the remaining acceptance item
  is deployed behavior, and only Decision 4's deployed proof discharges it.
- Will not serialize or shrink the owned list to dodge fetch volume: the
  per-bucket expectation and the one-request-per-second budget already
  bound the cost, and #10519 owns the real cadence fix.

## Not checked

- The owned-package trace for all four CVEs in the deployed system (asserted
  by the overlay comment; re-shown under Decision 4, item 2).
- Contemporaneous CVE Services / NVD records for the three supporting
  CVEs (only CVE-2026-96780 was re-read by the arbiter).
- Deployed QA state of any kind (facts, work items, source states, query
  surface, freshness): owed by Decision 4 after #10524 deploys.
- Whether a `cve_list` authority in fact registers in the deployed system
  and reaches `validateAuthorizedScanImpactEvidence` there (observed under
  Decision 4, item 7; the code-level omission on `origin/main` is verified).
