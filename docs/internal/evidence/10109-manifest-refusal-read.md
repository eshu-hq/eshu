# #10109 refusal-aware manifest reads: no-regression and observability evidence

Branch `lane3/10109-manifest-refusal` against base `ba8cbd3418`. The change
adds `ListActivePackageManifestDependencyFactsWithRefusals` plus the
`ManifestDeltaBaseRefusedError` taxonomy in
`go/internal/storage/postgres` and opts two retracting consumers into it
(package-source correlation fails whole-intent, supply-chain impact degrades
to partial). No new SQL text, index, statement, sort, scan, queue, lease, or
Cypher. The default delta-only read is byte-identical: `refuse=false` still
calls `overlayDeltaBase` (a wrapper over `overlayDeltaBaseWithRefusals` that
discards the refusals).

## No-Regression Evidence:

No-Regression Evidence: baseline (default read on the same stub shape) vs
after (refusing read) issues the same statements with the same texts. The
refusing read runs the same active pages + target lookup + base pages +
resolution + overlay inputs as the default read. The stub pin is
`TestListActivePackageManifestDependencyFactsWithRefusalsFailsOnAMissingBase`,
which asserts `len(db.queries) == 4` on the missing-base shape with the
comment "the refusal adds no read".

Backend/version: hermetic stub queryer (`fakeExecQueryer`), no live Postgres
in this proof (live Postgres execution of the new method is a separate
coordinator-owned gate and is NOT_CHECKED here). Go `go1.27.2 linux/amd64`.

Input shape: `packageManagers=["npm"]`, `packageNames=["left-pad"]`,
delta-active scope `repository:repo-1`, generation `generation-1`. The
missing-base shape feeds four stub responses: one active page row, the
delta-target lookup, an empty delta-base page, and an empty base lookup.

Row/terminal counts (asserted by the tests, all passing):

- Missing base: `loaded` rows `0`, `refused=true`, error is
  `*ManifestDeltaBaseRefusedError` (`ScopeID=repository:repo-1`,
  `GenerationID=generation-1`, `Outcome=union_base_missing`),
  `IsRetryable=false`, `FailureClass=
  manifest_dependency_delta_base_refused`, statements `4`,
  `union_base_missing` counter `1`.
- Scope mismatch: `refused=true`, `Outcome=union_scope_mismatch`,
  `IsRetryable=false`.
- Failed retention probe: `refused=false`, retryable error wrapping the
  probe failure (`context.DeadlineExceeded`), no `ManifestDeltaBaseRefusedError`.
- Legacy no-baseline: `loaded` rows `1` (the delta row), `refused=false`,
  `err=nil`.
- Package-source handler on refusal: `RepoDependencyIntentWriter` rows `0`,
  `WriteCorrelations` calls `0` (fails before any write or intent).
- Supply-chain impact handler on refusal: `PartialEvidence=true`,
  sub-signal `evidence_truncated_manifest_refused=1`, sub-signal
  `findings_retracted=0` (upsert only, retracts nothing).

Suite results measured in this worktree (2026-11-05, `go test -count=1`):

- Refusal set (`TestListActivePackageManifestDependencyFactsWithRefusals*`
  x4, `TestPackageSourceHandlerFailsBeforeEmitOnAManifestRefusal`,
  `TestSupplyChainImpactHandlerTreatsAManifestRefusalAsPartialEvidence`):
  6/6 PASS, exit 0 (`postgres 0.151s`, `correlation 0.013s`, `core 0.072s`).
- Full consumer suites `./internal/reducer/packages/correlation/`,
  `./internal/reducer/supplychain/core/`,
  `./internal/reducer/securityalert/`: `ok` x3, exit 0
  (`0.036s` / `1.400s` / `0.099s`).

Why safe: the refusal converts overlay outcomes that were already counted
and logged (`union_base_missing`, `union_scope_mismatch`) into an error
before rows reach a retracting consumer; it adds no statement (pinned at 4),
no per-scope N+1, no new sort, and no unbounded scan. Count-and-continue
outcomes (`union_no_baseline`, `union_base_unmatched`) and retryable probe
failures keep their existing behavior, and the existing default-read suites
pass unchanged.

## Observability Evidence:

Observability Evidence: delta-union counter
`eshu_dp_package_manifest_dependency_read_delta_union_total{read,outcome}`
records one decision per delta-active scope per read with attributes
`read` + `outcome`. The refusing outcomes are the bounded closed set
`union_base_missing` / `union_scope_mismatch`; the conversion to
`ManifestDeltaBaseRefusedError` does not re-count (the overlay records, the
conversion only reads). The missing-base test asserts
`union_base_missing == 1` via manual-reader collection
(`deltaUnionOutcomeCount`).

WARN fields: one WARN per refused scope from `warnDeltaOverlay`, subject
`manifest dependency read`, with `scope_id` (`log.ScopeID`),
`generation_id`, `delta_baseline_commit_sha`, `delta_union_base_commit_sha`,
and `chained`. Scope/generation/commit values never enter metric labels;
the error string carries scope, generation, read, and outcome but omits SHAs.

Impact truncation: cause `manifest_refused`
(`supplyChainImpactTruncationManifestRefused`) joins the bounded closed set
counted by `eshu_dp_supply_chain_impact_evidence_truncated_total`, plus a
WARN and sub-signals `evidence_partial=1` and
`evidence_truncated_manifest_refused=1`. The headroom gauge folds unknown
causes to `other`, so gauge cardinality stays 3.

Failure class: `manifest_dependency_delta_base_refused`
(`ManifestDependencyDeltaBaseRefusedFailureClass`) surfaces as the durable
failure class on the dead-lettered package-source intent (non-retryable;
a retry reads the same refused base, which heals only at the refused scope's
next full generation).

Span event: one `package_manifest_dependency_read.delta_union` event per read
that reached a delta-active scope, carrying the per-outcome decision counts
(one event per read, not per scope).
