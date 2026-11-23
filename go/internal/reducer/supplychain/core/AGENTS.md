# core — agent instructions (issue #6061)

This directory is the supplychain family. It is mid-restructure:
the tree doc (`docs/internal/design/reducer-target-tree.md`) is the real
target; this package is step 2 (supplychain-core, after
packages/correlation) and the code, cloud, workload, and remaining families
still live in the reducer root and must never import this package. The
tree doc's sequencing step 2 defines the done bar; re-read it before
changing anything here.

## Invariants

- Never import the parent reducer package, and never let the shared tier
  (`contract`, `factload`, `factdecode`, `factwrite`, `schemadecode`,
  `crossscope`, `payloadcore`, `supplychainmodel`) import this package. The
  `cicdrun`, `containerimage`, `servicecatalog`, `correlation`, `source`,
  and `securityalert` imports stay one-way (this package reads their
  exported consts, fact kinds, and alert/consumption types; none of them
  imports this package).
- Package moves and mechanical refactors preserve behavior: same intents
  admitted, same facts written, same metrics emitted. Prove it with the
  focused family tests plus the reducer-root callers
  (`defaults_additive_domains_*`, `defaults_handlers.go`, `service.go`).
- A behavior change here is deliberate and proven. A bug fix starts from
  a regression test that fails before the fix. New behavior ships with
  tests of its contract. Both keep the family tests and the reducer-root
  caller tests green, leave unrelated ownership, evidence, and telemetry
  contracts as they were, and record the new rule in this package's
  README and in the public doc that describes the changed status or
  classification.
- A `supply_chain_impact` intent whose payload carries
  `trigger: manifest_dependency` (#9874) never runs the findings pass: Handle
  routes it to `handleManifestTrigger` (`manifest_trigger.go`) before the
  fencing token, and that branch only reopens holders. Keep
  `beforeEvidenceLoad` (`attachment_wait.go`) the one call site and the projector spelling equal to `PayloadKeyTrigger` /
  `TriggerManifestDependency`; any implementer of `SupplyChainImpactFanOut`
  must also implement `FindPackageHolderCandidates`, and the production port
  must implement `ManifestDependencyGenerationReader` (generation read plus the
  lease-fenced `RecordManifestTriggerCompleted` stamp, plus the marker-free
  `FindInFlightNonMarkerScopes` in-flight read, which the findings fan-out
  reads too: nothing reopens or flags an in-flight marker, #10972). Stamp only when no
  holder is left on the continuation list or nothing changed; the next marker
  pass of the scope diffs from the newest stamped generation.
- A findings pass never loads evidence while its own scope generation's
  `sbom_attestation_attachment` work is in flight (#11105,
  `attachment_wait.go`): the probe runs in `beforeEvidenceLoad`, ahead of the
  fencing token and the #5709 floor sample, on every attempt. Keep it there.
  A probe read after the load would miss an attachment that committed in
  between, and nothing re-runs the pass. The deferral class
  `SBOMAttachmentNotReadyFailureClass` must stay in
  `nonCountingReducerRetryFailureClasses` and in `WaitingDeferralMap` as a
  producer-terminal wait narrowed to `sbom_attestation_attachment`. A probe
  error stays counting (`factload.ClassifyFactLoadError`), and the bound is
  elapsed time from the attachment row's last-workable time, never an
  attempt count.
- New exports are join contracts other families program against. Name them
  for what they return, document who reads them, and keep the bodies
  cut-paste identical to what they replace.
- Test doubles stay family-local: Go test files cannot share unexported
  symbols across a package boundary, so each side keeps its own copy of
  shared fakes and fixtures (see
  `cross_scope_test_doubles_test.go` here, and the
  staying twins in the parent's `*_test_doubles_test.go` /
  `*_test_fixtures_test.go` / `defaults_cross_scope_readiness_wiring_test.go`
  files). Shared test decode for batched versioned writes lives in
  `factwrite/testutil` — extend that leaf instead of copying decoders.
- The winners keyed resweep (#9805) is sound only while no payload field the
  winners select reads is outside the finding content digest, because the
  writer skips the dirty-log capture of a rewrite with an unchanged digest.
  Adding a payload field to the digest exclusions in `contentDigestOf`, or a
  new field to the winners select that the digest leaves out, breaks it; the
  digest-coverage test and the postgres select guard fail on both.
- Digest-less OS rows, and each digest's rows, classify one (release line,
  installed version) group at a time (#9984, #10057,
  `splitOSPackagesByReleaseLine`). A shard keeps only each lookup key's least row
  by `compareOSPackageRows`, because this path has no SBOM component and the
  classifier reads one row. A new reader of a shard's rows must change that
  first. A digest attached to an SBOM subject is different: it reads every
  matching version through `matchingOSPackageImpactPaths`. Coalesce stamps the SBOM version conflict label only when
  `findingHasSBOMComponentPath` holds for one of the merged findings; a merge
  of digest-less lines whose purl has no distro qualifier keeps the lesser reason.
- A repository's manifest rows for one package classify once per distinct
  observed version (#11113, `splitRepositoryConsumptionByVersion`), keyed by
  `consumptionObservedVersion`, the same rule that sets the finding's
  `observed_version`; keep the two on that one function. Do not drop
  `observed_version` from the writer or winners key, dedupe versions at query
  time, or rank one "best" version: each installed version is its own finding.
  Rows with no version keep the one-finding rule only when no row of the
  repository carries a version. The provider-alert path follows the same
  rule over the alert manifest's rows (#11121,
  `appendSecurityAlertVersionFindings`): pick within a version only through
  `securityalert.SelectSecurityAlertConsumption`, and keep
  `securityAlertImpactAlreadyExists` comparing the observed version (a
  versionless alert row keeps the any-version rule).
- `telemetry-coverage.md` rows that name files in this package must keep
  pointing at real files with a net-zero row count; the dirgate row for
  `internal/reducer` ratchets DOWN on family moves and never absorbs new
  files to hold this package's count.
- `withJoinableOSRepresentative` (#9990) narrows a group to the facts that join
  in two cases, both rows this change creates: a representative that joins no
  installed row (the group yields no finding without it), and a representative with
  a blank ecosystem that joins, with a version on every row it joins and a fact
  beside it that joins nothing. Widening it to a group with an SBOM path or a
  consumer, a spelled ecosystem that joins, a row with no installed version, or a
  group whose facts all join moves findings that exist today or ranks a pick that
  #10034 item 1 owns. A blank group whose joined rows are mixed, with and without a
  version, goes through `splitDirectBlankGroupByRowVersion` first (9990-f6): condition c
  looks at one index, and a row with no version must not keep the whole-group read for
  the versioned rows beside it, which have no base row. Split by row, never by shard: a
  digest shard can hold both kinds. The versionless index has no outcome map, so a
  group counts once. It returns a new slice of the joining facts and never edits its
  input: the index shares the group across installed rows and scan targets, and
  another target may join another fact. Add a new anchor source to
  `supplyChainImpactFindingHasOwnedAnchor` and to this gate together.
- `recordEvidenceHeadroom` feeds the #9778 headroom gauges through
  `Instruments.SupplyChainImpactHeadroom` after an admitted write only. Keep it
  after the write and on `truncation.causes()`, the same set that decides
  `PartialEvidence`, so the gauge never disagrees with retraction. The ratio reads `budgetEnvelopes` (`budget.used`), never `expansionEnvelopes`: the protected-scan path adds uncharged derived facts to that one.
- The re-key outcome (`rekey_signals.go`, #10408) is a closed four-set
  (`merge`, `split`, `member_only`, `retire_only`) reporting a genuine
  identity change only — `reportRekeyIdentity` compares each derived row
  against the stored row it replaced, so an EPSS refresh, admission-only,
  bootstrap, rollover, or same-key rewrite counts nothing — and counted
  once per pass at the dispatcher. Never put a group, CVE, or advisory id
  in a metric attribute; magnitudes ride the `rekey_*` result sub-signals.
  A pass with no identity change stays silent — unless it dropped
  suppression coverage, which reports `split`: do not default a silent
  pass to `member_only`.
- Suppression identity matching (#10409) tests each populated scope
  `cve_id`/`advisory_id` against the finding's matchable set (group member
  ids plus its own `cve_id`/`advisory_id`) via `findingMatchableID`,
  conjunctive per field; never widen through `upstream`/`related`, and
  never match a fallback finding through a partial component. Keep the
  per-pass `suppressionIndex` a pure prefilter (superset of adjacency,
  input order preserved, non-ASCII or anchorless scopes on the full-scan
  fallback) — `TestSuppressionIndexMatchesFullScan` is the parity proof.
  Decisions carry `matched_id`/`matched_field`/`matched_via`; diff rows
  carry advisory ids on both sides, and the no-identity-scope counter
  counts only suppressions naming neither id.
- `FileReachabilityFold` (#9781, #9860) replaces parser `file` envelopes with
  in-memory digest envelopes. It runs the same per-file bodies as the JS/TS and
  Python builders (`addFile`, `addPythonReachabilityFile`), so a new key a
  builder reads is read by the fold with no second edit. Fold in the order the
  pass would append (first usage of the best priority wins; Python lists keep
  fact order). The active-evidence digest goes after earlier files and before
  the Python load; the Python-stage digest (#9860, distinct fact ID, same kind)
  goes where the loaded files would have gone, at the end. A fold has one owner
  goroutine and no locks. Digests never reach a writer;
  `TestFileReachabilityFoldMatchesEnvelopeBuilders` is the active parity gate
  and `TestLoadPythonReachabilityEvidenceFactsFoldsPagedFiles` the Python one.
- Go reachability facts are built from recorded `govulncheck -json` output in
  `testdata/govulncheck` (#10028), not hand-written payloads. The reducer reads
  level `import` as `reachable`, which is right only for a package-level scan;
  a symbol-level scan reports "imported, never called" as `not_called`, and the
  emitter (eshu-scanner-worker) must never write `import` or `module` for a
  symbol-level scan. `stdlib` advisories have no `go_module_evidence` row, so
  they produce no impact finding. A failed govulncheck run emits no
  reachability fact; when the reducer joins the run's
  `scanner_worker.warning` (emitter-stamped `repository_id`, unfinished
  `analysis_status`, `go_reachability` analyzer) into a finding whose fact
  state is `not_called`, `module_only`, or `unknown`, the verdict reads
  `analysis_failed` (`Reachability` state `missing_evidence`), with the
  warning rendered as `govulncheck scan <reason>`, which rides
  `Reachability.MissingEvidence` to every read surface (#10029 R2, #10043).
  A reachable fact verdict (`symbol_reachable`,
  `package_import_reachable`) wins untouched and takes no warning entry.
  No-Regression Evidence (#10043): throwaway `ClassifyGoVulnerabilityReachability`
  bench, 200 findings + 1 joined warning, same host (linux/arm64, go1.26.6),
  base 86b2adcfa7 vs head — finished leg 760us/894KB/10671 allocs vs
  747us/888KB/10271 allocs (reachable wins, warning no longer appended);
  failed leg 480us/559KB/6859 allocs vs 505us/581KB/7259 allocs (~125ns and
  ~110B per flipped finding for one extra append plus the reason string).
  O(1) extra work per joined finding; the classifier runs once per reducer
  pass. No-Observability-Change: no new metric, span, log key, queue, lane,
  or knob; the signal rides the existing `Reachability.State` /
  `RuntimeReachability` fields end to end.
  The join is scoped by the warning's
  `relative_path` (#11002): blank or whitespace-only keeps repository
  scope, while a non-blank path byte-matches (no trimming, no case or
  separator folding) the `go_module_evidence` rows behind each finding and
  joins only the findings with a matching row, rendered as
  `govulncheck scan <reason> at <relative_path>`.
  Scan provenance (`scan_level`, `scan_mode`, `scanner_version`,
  `trace_count`) rides the typed decode to the explain payload, one key
  each and only when the fact reports it; pre-provenance facts emit none
  of the keys (#10028).
- The OS advisory grouping pass (#9914, `groupOSAdvisoriesByUpstream`) is the
  only place a distro record is filed under a CVE id. Keep it in the index
  build: never in verdict code, never by changing the producer's `cve_id`, and
  never from the raw payload (it reads the typed `Upstream` field only). It must
  stay zero-allocation for a record that is not an OS vendor record and carries
  no `upstream` list (`BenchmarkBuildSupplyChainImpactIndexUpstreamGrouping`).
  It joins on `AdvisoryID`, never `cve_id`: an alias can give unrelated records
  one `cve_id`. Changing the key rule moves `finding_id`, the winners key and
  `cve_id` filters for every grouped OS finding, so treat it as an identity
  migration.
- `UpstreamCVEIDs` on the load filter asks for the CVEs an `upstream` list names
  and reads KEV, EPSS and suppression scopes only. It reads the raw payload, because
  the filter runs before the typed decode, and it forms no group. Do not move an
  upstream id onto `CVEIDs`, directly or through `appendSignalCVEIDs`: that loads
  the CVE's NVD record and product rows, the widening the #9914 shim failed. Widen
  only with a budget measurement on a store that holds NVD data.
- An OS-class finding is named by `decidingObservation`, not `IdentitySource`, so
  its identity does not depend on what the pass loaded. Do not let a scored source
  rename one, and do not move this rule to other ecosystems: #9693 pins theirs.
- Among live records the one whose range holds the installed version decides, highest
  fix first, and only its fix and range are offered. `osVerdictComparator` must stay
  the comparator `evaluateSupplyChainVersionMatch` uses for each family, or a record
  can be named by a different order than the one that set the verdict.
- An OS-class finding judges retraction over the records listing its package
  (`recordsListingThePackage`). Do not widen that to language ecosystems here: the
  base behaviour there is not this rule's defect, and changing it needs its own proof.
- A fact that cannot anchor a finding on an index must not change the finding that
  index would produce without it (`onlyRetractedRecordsAnchor`, ruling 9914-r5). Pass
  `liveAdvisoryPackages` the index that classifies the group, or the two paths disagree.
- A record is retracted as a whole (`retractedRecordOf`, ruling 9914-retract): a fact that
  pairing left without a cve observation takes its record's lifecycle only when every cve
  fact of that source and advisory id retracted. A source with no cve fact stays live, and
  a retracted finding offers no fix, so the authorized pass selects no branch for one.
- A consumer that reads affected packages reads them from the index, after
  `liveAdvisoryPackages`, as the primary path and the derived conflict gate do. A
  second typed decode sees the producer's `cve_id` and misses every grouped
  finding.

No-Regression Evidence (#11002 module-scope join): `go test
./internal/reducer/supplychain/core/ ./internal/query/supply/chain/impact/
-race -count=1` green on the rebased head (core 8.6s, impact 1.1s with
race; 0.8s/0.1s without). The hot-path delta is an in-memory byte-equal
filter over already-materialized module rows plus one readiness CTE `AND`
equality over rare warning rows: no new query, index, lock, or allocator
shape, so no measurement beyond the suite timings is owed.

No-Observability-Change (#11002): the join narrowing adds no metrics,
spans, or logs; failure classes and quarantine behavior are unchanged.

No-Regression Evidence (#11247 audit tick budget clock): `runAuditOnce` reads
its deadline from `budgetNow()`, which is the injected `Now` when set and
`time.Now` otherwise, so the default path is the same clock reads as before:
one at tick start plus one per pass (monotonic reading kept; `now()` is
not used because `UTC()` strips it). No query, lease, claim, or lock call is added, moved, or
reordered. `go test -race -count=1 ./internal/reducer/supplychain/core` is
green (about 10 s), and the budget test is exact on a fake clock: 4 passes in
a 70 ms budget, 3 when a pass ends on the deadline, 1 when the budget is
shorter than a pass. Before: the wall-clock test failed 3 of 20 runs under
a 128-process CPU hog; after: 0 of 20.

No-Observability-Change (#11247): no metric, span, log key, or setting is
added or changed.

No-Regression Evidence (#11111 cut-load deny sweep, #11177 guarded-full count):
the deny sweep on a cut-load pass is the existing sweep body (`dropAliasDeniedRecords`)
run over the records already in memory, so it adds one linear pass and no
query, lease, claim, or lock call; with no deny list a cut pass still returns
at once (`TestAliasCutLoadWithoutDenyKeepsIndexUntouched`). `runContractTick`
reads the `rebuilt` flag `runTick` already returns, so the tick itself is
unchanged. `go test -race -count=3 ./internal/reducer/supplychain/core` is
green (about 49 s at load 28). Before: the three new regression tests failed
on the base sources; after: they pass and five mutants fail them.

Observability Evidence (#11177): the existing winners full-resweep counter and
its log line now fire only when this tick committed the full resweep, not when
the lease was not claimed or the tick stood down; the meaning is stated in
docs/public/reference/telemetry/supply-chain-impact-winners.md. #11111 reuses
the existing closed `result="denied"` label on the alias-group counter. No new
metric, label, span, or setting.

No-Regression Evidence (#10972 markers out of the CVE fan-out): the findings
fan-out's in-flight read now runs `findInFlightNonMarkerScopesQuery`, the
shipped in-flight statement plus one residual `payload->>'trigger'` predicate on
the rows the same `(stage, domain, status)` index scan already returns; the
set is bounded by the passes in flight, and it is one read per fan-out as before.
Fewer in-flight scopes reach the reopen loop, so a marker costs no reopen
statement and sets no replay bit. No lock, lease, claim, or ordering change.
`TestFanOutNeverReopensAnInFlightManifestMarker` was red on the base sources
(reopened an in-flight marker) and is green now; the live twins
(`TestFindInFlightScopesExcludesManifestMarkersLive`,
`TestManifestMarkerFlagsNeverComeFromACVEFanOutLive`) run in the
coordinator's slot.

Observability Evidence (#10972): no new signal on this path. The existing
fan-out holder counters and `fanout_holders` log field now exclude in-flight
markers; marker executions per hour stay on the `manifest_trigger` signal. The
completion fan-out's skipped-marker counter is described in
`go/internal/storage/postgres/scope/completion/README.md`.
