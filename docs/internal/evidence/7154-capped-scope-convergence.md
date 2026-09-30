# Supply-Chain Impact Capped-Scope Convergence (#7154)

## Defect

#6831 made each supply-chain impact pass the complete truth for its
`(scope, generation)`: it upserts its findings and tombstones every other
active finding row of that pair. A pass whose evidence load hit a bound
(`SupplyChainImpactWrite.PartialEvidence`) upserts only, because a row absent
from a partial pass's keep set is ambiguous: superseded, or merely not reached.

The bounds fired for scopes that are simply large. The OS-package reader
stopped at 500 targets, the scanner-analysis stage at 256 scan scopes, the
resolved-digest and peer-identity stages at 256 values, and a bounded
`vulnerability.suppression` tail set the same flag. A scope over any of them was
partial on every pass, so its superseded findings were never retracted and
stayed visible on the query surface.

Root-Cause Evidence: the live proof
`TestSupplyChainImpactCappedScopeConvergesLive` seeds 1203 installed OS-package
targets and a stale finding row in the pass's `(scope, generation)`, then runs
the real handler over a real `FactStore`. On unmodified main
(`725fa19425`) the pass loaded `os_package_advisory_facts = 500` of 1203, wrote
`PartialEvidence = true`, and the stale row stayed active
(`is_tombstone = FALSE`). With the change the pass loads all 1203, retracts the
stale row, and keeps the finding it derives.

## Fix

The arbiter ruling was: make the pass complete rather than make the retraction
cleverer. Retraction, its SQL, its keep set, and the transaction shape are
unchanged.

- Truncation is recorded by cause (`supplyChainImpactTruncation`,
  `supplychain/core/evidence_truncation.go`). Only `active_expansion_rounds`
  (the 8-round expansion cap) and `evidence_budget` (a per-intent budget of
  expansion envelopes, default 100,000) set `PartialEvidence`. A
  `suppression_tail` truncation does not: a suppression is not part of a
  finding's identity, and core evidence is always loaded in full before the
  suppression tail is counted, so the pass still retracts and only discards its
  suppression candidates (findings fail open).
- The scanner-analysis stage loads every distinct scan-target pair. The
  resolved-digest and peer-identity stages read their values in chunks of 256
  instead of dropping the excess. A filter matches a row when any of its values
  does, so the union of chunk reads equals one read of all values.
- The OS-package read is narrowed to the intent's affected packages and drained
  inside one snapshot. The finding set of an intent depends only on installed
  rows whose package id equals one of its affected packages' lookup keys
  (`affectedOSPackageLookupKeys`: the package id, the package id derived from the
  purl, and the vendor's `os://` identity), so
  `ListOSPackageAdvisoryFactEnvelopes(ctx, ecosystems, packageIDs, limit)` reads
  only rows whose installed package id (the purl trimmed and cut at its first
  `@`, exactly `core.packageIDFromPURL`) is one of those keys. An intent with no
  usable key reads nothing and is not partial. The predicate is a materialized
  candidates query over a partial expression index
  (`fact_records_os_package_purl_prefix_idx`, migration 153), pinned to the
  query text by a test. The drain pages by fact id inside one read-only
  repeatable-read snapshot and stops on the caller's remaining evidence budget,
  keeping the page that crossed it.
- The snapshot matters because an `os_package` fact id hashes its generation id.
  When a scan scope's generation flips between two page statements, every
  still-installed package moves in the key space and a per-statement cursor
  skips some of them; the pass would then count as complete and retract their
  findings, and no intent re-runs it. One snapshot reads one consistent
  generation set. The alternative, re-checking at the end that every generation
  seen is still active, was rejected: it detects the tear after paying for it and
  adds a cause that never converges under scan churn.
- Narrowing also narrows what the scanner-analysis, resolved-digest and peer
  stages expand: only scans holding a key-matching installed package. Finding
  status, `RepositoryID` and `SubjectDigest` do not change; OS anchoring uses
  `scannerAnalyses` of the matched scope and images loaded by digest. One part of
  the finding content does: identity reconciliation (#5468) compares the scanner
  digest with every identity in the evidence set whose single source repository is
  the finding's, however it was loaded. It now sees only identities reachable
  from the matched scans' digests plus same-OCI-repository peers (the peer stage
  keys on the identity's `repository_id`). A same-git-source identity under a
  different OCI repository that only an unrelated scan would have loaded is no
  longer compared, so its `scanner_identity_digest_mismatch` line is no longer
  emitted. That population is deterministic and is the peer stage's designed one;
  the wider one a full drain produced was an accident of load order, and on main
  it was the installed packages holding the 501 lowest fact ids of the ecosystem
  (the base reader passed no rotation offset and capped at 501). No consumer
  outside the reconciliation function and its unit test reads the mismatch
  string. The committed two-arm test
  `TestSupplyChainImpactNarrowedReadReconcilesAgainstMatchedScansAndSameOCIPeers`
  pins exactly this, with the digest and peer stages live in both arms: the full
  arm emits mismatch lines for the same-OCI peer and the unrelated scan, the
  narrowed arm only for the peer. The `scanner_analysis_scope_facts` sub-signal
  drops accordingly. Whether same-source, cross-OCI digests are a pipeline
  disagreement or monorepo noise is an open semantic question that no issue
  tracks today; this change accepts the narrowed population as designed and
  does not decide it.

## Lock order and transaction scope

Unchanged from #6831 (`writer_retract.go`): one transaction per pass; the
`pg_advisory_xact_lock` on `(fact_kind, scope, generation)` is taken first,
before any row lock, then the batched upsert, then the retraction, then commit.
All new paging happens inside `loadSupplyChainImpactEvidence`, before
`BeginSupplyChainImpactTx`, so no lock or transaction is held while evidence
loads. The lock's hold time grows only with the finding count, as before. The
retraction predicate, its `fencing_token <= $5` guard, and the argument list are
byte-identical, so #7142 adds its token without rework. A partial pass (rounds
or budget) still upserts and retracts nothing, so it cannot widen the
#7142 stale-retraction window.

## Edge cases

| Case | Complete pass | Rounds or budget pass |
| --- | --- | --- |
| Multi-anchor subject (repo A and repo B) | both derived and kept | rows only added |
| possible to affected flip | new fact id; old tombstoned | old survives beside the new one, marked |
| repo-less to anchored | old twin tombstoned (#6831 proof) | same as today |
| Suppression tail truncated | retracts; suppressions discarded, findings fail open | unchanged |
| Empty finding set | retracts every active row | nothing |
| Other generation or fact kind | untouched | untouched |
| Partial pass concurrent with a full pass | the advisory lock serializes; the later full pass retracts the partial pass's extras | a later partial pass only adds rows |
| Formerly capped scope (over 500 targets, 256 pairs/digests/repo ids) | complete, retracts | not applicable |
| Scan generation flips mid-drain | one snapshot reads the pre-flip set exactly; the flip is picked up by the next pass | not applicable |
| Intent with no OS affected package, or none with a lookup key | reads nothing, not partial | not applicable |

## Rejected and deferred

- Retract the rows a partial pass can prove superseded (same identity minus
  repository, status, or digest): unsafe for a subject with two anchors, because
  a truncated pass that derives only repo A would retract repo B, and those
  fields are classifier outputs of evidence a partial pass may lack.
- Alert or raise the caps only: never converges.
- Retraction per evidence window with a completeness marker: needs a closed key
  set per window, and blank-anchor rows and the OS reader have none. Deferred
  until a measurement shows scopes above the budget at realistic size.
- Reading every installed package of the ecosystem for every intent (the first
  version of this change, a keyset pager filtered by ecosystem only): correct but
  it made every intent pay a fleet-wide drain. Measured below, then replaced by
  the narrowed read.
- A partial index on `fact_records (fact_id) WHERE fact_kind =
  'vulnerability.os_package' AND is_tombstone = FALSE` to make each
  ecosystem-wide page stop early: measured, and the planner ignores it.
- An exact-prefix predicate driven from the scope join: the planner
  misestimated the join and read every row on every page even with the
  expression index present. The materialized candidates query is what makes the
  index effective.

Performance Evidence: local Postgres 18 (`postgres:18-alpine`, throwaway
container), synthetic corpus of 50,000 active `vulnerability.os_package` facts
over 5,000 active scan scopes (10 package names per scope, one ecosystem,
`ANALYZE`d), plus one scanner analysis fact per scope and a debian CVE with one
affected package in one intel scope. Reproduced with a scratch shim run through
the real `FactStore` and `SupplyChainImpactHandler` (not committed). "Before" is
the first version of this change (ecosystem-only keyset pages); the code on main
stopped at 500 targets and marked the pass partial, so its totals are not
comparable, it was fast because it was wrong.

- Per page (`EXPLAIN (ANALYZE, BUFFERS)`, 500-row page): ecosystem-only keyset
  read, 60,254 buffers and 28-72 ms on every page. Narrowed read, 2 keys: 869
  buffers and 4.8 ms. Narrowed read, 100 keys: 3,033 buffers and 5.6 ms.
- Rotation query for comparison (unchanged, used by the coordinator planner): 167
  ms with a 12.4 MB external sort.
- One intent whose affected package matches 5,000 installed rows: whole pass
  3.02 s wall, `load_os_package_advisory` 0.056 s, `load_scanner_analysis_scope`
  1.99 s (5,000 sequential scoped reads), `load_resolved_digest_evidence` 0.90
  s, 1 finding, no truncation. Before: the same corpus with the ecosystem-only
  drain was 11.99 s (7.04 s OS stage), reading all 50,000 installs for an intent
  that matches 5,000.
- Drain cost by installs of the affected package (review measurement through the
  real `FactStore` drain on a 1.3-million-row `fact_records`, generic plan,
  7 runs per size): 2 installs, about 2 ms; 5,000 installs, 0.10 to 0.29 s;
  100,000 installs (the budget ceiling), 8 to 16 s. The cost is superlinear in the
  installs of one package, not proportional: the lookup carries at least two keys
  when a purl and a name exist (`pkg:deb/debian/X` and `os://debian/X`), and with
  `= ANY` over several keys the `(expression, fact_id)` index cannot return rows
  in `fact_id` order, so each 500-row page sorts every remaining match. The whole
  drain also holds one read-only repeatable-read snapshot, which pins the xmin
  horizon for its duration. Realistic per-package install counts (hundreds to low
  thousands of scans) stay well under a second. A drain that walks one key at a
  time, or orders by `(expression, fact_id)`, would make each page O(page); it is
  not needed at the measured sizes and is the follow-up if a package installed in
  tens of thousands of scans proves slow.
- One scanner read per matching scan scope remains, sequential; batching them
  into one multi-scope read is a follow-up if a package installed in very many
  scans proves slow.
- At the scale limit: a single affected package installed in more than 100,000
  scan targets would spend the 100,000-envelope evidence budget and stay partial
  (so it does not converge), with the WARN counter naming the scope. That is a
  documented permanent constraint, far above what the ecosystem-wide read hit.
- Budget arithmetic: heap grew 101 MB over 55,002 envelopes in the first
  version, about 1.9 KB retained per envelope, so the 100,000-envelope default
  holds about 190 MB per in-flight pass at the ceiling. The chart's
  resolution-engine limit is 32 GiB, so 25% of it (8 GiB) covers more than 40
  concurrent passes at the ceiling. The narrowed pass above held 31 MB.
- The budget is a documented constant, not configuration:
  `SupplyChainImpactHandler.EvidenceBudget` overrides it for tests and is not
  wired to an environment variable. An operator who sees
  `eshu_dp_supply_chain_impact_evidence_truncated_total{reason="evidence_budget"}`
  rising has a scope whose single affected package is installed in more than
  100,000 scan targets; a knob is a follow-up if that measurement ever appears.
- The budget counts expansion envelopes only (the active-evidence, OS-package,
  scanner-analysis, resolved-digest and peer-identity stages), never the intent
  scope's own base load, so a large vulnerability scope is not marked partial
  for its size.

Test Evidence:

- Core, hermetic: `TestSupplyChainImpactOSPackageTargetsOverCapConverge`,
  `TestSupplyChainImpactPeerIdentityRepositoriesOverCapConverge`,
  `TestSupplyChainImpactResolvedDigestsOverCapConverge`,
  `TestSupplyChainImpactScannerScopePairsOverCapConverge` and the flipped
  `TestSupplyChainImpactHandlerFailsOpenWhenSuppressionCandidatesAreTruncated`
  failed on the old code (`PartialEvidence = true`, `truncated = true`) and pass
  now. `TestSupplyChainImpactNarrowedOSPackageReadDerivesTheSameFindingsAsAFullDrain`
  is the differential proof for the OS-package and scanner-analysis stages (its
  active-evidence fake loads no identities, so it says nothing about
  reconciliation, which the two-arm test above pins);
  `TestSupplyChainImpactOSPackageAdvisoryTargetsDeriveLookupKeys`
  and `...StageSkipsTheReadWithoutLookupKeys` pin the key derivation and the
  empty-key skip. `TestSupplyChainImpactOSPackageBudgetSpentMakesThePassPartial`
  and `TestSupplyChainImpactChunkedStagesStopWhenTheBudgetIsSpent` pin the
  budget: deleting the fold of an exhausted budget into the truncation, or
  making the digest stage ignore the budget, turns them red (both mutations run).
  `TestSupplyChainImpactRoundCapSkipsRetractionAndSignals`,
  `TestSupplyChainImpactEvidenceBudgetSkipsRetractionAndSignals` and
  `TestSupplyChainImpactSuppressionTailIsNotEvidenceTruncation` pin the causes,
  the counter, and the WARN log.
- Binding: `os_package_advisory_loader_binding_test.go` asserts at compile time
  that `postgres.FactStore` satisfies the reader interface the handler reaches
  through a runtime type assertion, so drift fails every PR instead of silently
  skipping the stage.
- Storage, hermetic: the SQL trim set equals the runes `strings.TrimSpace`
  strips (`TestOSPackagePURLTrimSetMatchesGoTrimSpace`, enumerating all runes);
  the query expression equals migration 153's index expression
  (`TestOSPackagePURLPrefixExpressionMatchesMigration`); the drain reads one
  snapshot, advances its cursor, stops on the limit keeping the crossing page,
  counts skipped rows, is not ended by a page of inactive rows, and refuses a
  full page that does not advance.
- Live Postgres (ledger class `scheduled`):
  `TestListOSPackageAdvisoryFactEnvelopesNarrowedPagesToCompletionLive` (1203
  targets returned once each, inactive generations read past, a narrow key set
  returns only its own rows), `...PURLFormsMatchTheGoMatcherLive` (eighteen purl
  forms including U+00A0, tab, em space and ideographic space padding: the SQL
  rows equal the Go matcher's rows for every key),
  `TestOSPackageNarrowedQueryUsesTheIndexAtScaleLive` (the plan uses the
  expression index at 50,000 rows and never a sequential scan),
  `...GenerationFlipMidDrainLive` (a generation flip committed between page one
  and page two: the drain returns exactly the pre-flip set; a snapshot per page
  instead of one returns 500 of 1203 and fails), and
  `TestSupplyChainImpactCappedScopeConvergesLive` (also derives an OS finding
  from a target past the first page).

Observability Evidence: a pass stopped by a cause that blocks retraction
increments `eshu_dp_supply_chain_impact_evidence_truncated_total` with labels
`domain` and `reason` (`active_expansion_rounds` or `evidence_budget`) and logs
one WARN line, "supply chain impact evidence truncated; findings not
retracted", carrying `scope_id`, `generation_id`, `intent_id`, `cause`,
`evidence_envelopes`, `expansion_envelopes`, and `findings`. A scope stuck on
every pass shows as `increase(...[6h]) > 0` with the scope in the log line. The
result sub-signals add `evidence_truncated_rounds`, `evidence_truncated_budget`,
`suppression_evidence_truncated`, `evidence_envelopes`, and
`evidence_expansion_envelopes`; `active_evidence_truncated` now means an
identity-affecting truncation only. A suppression-tail truncation is visible as
`suppression_evidence_truncated` and in the evidence summary but is not counted,
because it does not stop retraction.

No-Regression Evidence: the retraction SQL, keep set, argument list, advisory
lock, and single-transaction shape are unchanged, so the #6831 live proofs
(`TestSupplyChainImpactWriter*Live`) pass unmodified. The per-intent cost of
the OS-package, scanner-analysis and digest stages now follows the installs of
the intent's affected packages (superlinear in one package's installs, see
Performance Evidence) instead of stopping at a cap; the measured cost at 50,000
targets is above.

Not checked: the golden-corpus gate's maximum `evidence_envelopes` per pass
(the ruling asks it stay under 10% of the default budget; that gate runs in CI
only) and any ops-qa scope distribution. This lane has no ops-qa access; the
budget default rests on the memory arithmetic above.

Observed, not proven on main and not filed: the arbiter read that live OSV
Debian purls carry `?arch=source`, in which case `packageIDFromPURL` of an
advisory purl would not equal an installed package's id and OSV-to-dpkg matching
would find nothing in production. The lookup keys come from
`affectedOSPackageLookupKeys`, so a matcher fix carries into the reader without
a SQL change.
