# #6782: transient-state reads are excluded by registration (option 1)

Owner direction 2026-09-22: implement options 1+2 of the transient-read
characterization (issue #6782, 11:29 table note). Option 2 (rowcount
advisory) is proven in `6782-rowcount-advisory.md`; this slice is
option 1. Both ride PR #6971.

## Symptom

PR #6971 run 35783305842 attempt 2 (job 106951019393 `differential
nornicdb vs neo4j`, head 796bca18d) failed quorum with 1 reproduced
divergence:

`MATCH (n:Module) WHERE n.evidence_source IS NOT NULL AND n.uid IS NULL
AND (n.name > $cursor_0 OR ...) ... LIMIT $limit`:
`row digest differs (nornicdb=427 rows, neo4j=406 rows)`.

This is the first page of the uid-less Module orphan scan. Modules not
yet resolved to a uid are transient state: the page depends on where the
parse/resolution drain is when it is read. The same statement diverged
(406 vs 413) in attempt 1's pairing 2 without reproducing, and the
EvidenceArtifact orphan-sweep page diverged (88 vs 78) the same way —
quorum reproduces either whenever both pairings land off the agreeing
point. A plain allowlist entry cannot cover this: on agreeing runs the
entry matches nothing and fails the gate by the stale-entry rule.

## Change

`transient_reads` section in `specs/backend-divergence-allowlist.v1.yaml`,
parsed by `capture.ParseAllowlist` beside `entries`
(`go/internal/graph/capture/allowlist.go`):

- Fingerprint-keyed like the allowlist (statement equality, optional
  parameter narrowing), with the same reason and upstream-issue
  accountability.
- No tier key (strict-decoded: a tier is a parse error, not a silent
  no-op) and never stale-checked — the read agrees on most runs by
  design.
- Parse guard: the statement must carry a transient-state marker
  (`uid IS NULL`, `eshu_orphan_observed_at_unix`), so a steady-state
  read can never be registered by accident.
- Kind-scoped exclusion (`Allowlist.ExcludeTransient`): only
  results/rowcount/executions divergences are held; a backend error or
  a one-sided recording on a transient read stays required.

Committed registrations (2): the Module orphan first page and the
EvidenceArtifact orphan-sweep page — the two reads evidenced divergent
above. The orphan-marking write and the un-evidenced Repository,
Platform, File, and Directory sweep pages stay under full truth
comparison.

Wiring: `capture.Compare` and the quorum per-pairing path exclude
transient divergences before quorum intersection and report them in the
visible non-required `nornicdb_vs_neo4j_transient` advisory finding (own
finding, because the executions advisory's "agreeing results" wording
would be false for digest-differing reads).

## RED/GREEN proof commands run

- `go test ./internal/graph/capture/ -run Transient`: RED before
  (ExcludeTransient undefined; transient section unparsed), GREEN after
  — 5 tests: guard rejection, tier rejection, orphan hold, failures +
  missing kept required, not-stale-checked.
- `go test ./cmd/golden-corpus-gate/ -run
  TestRunBackendDiffQuorumTransientOrphanPasses`: RED before (quorum
  `2 pass, 1 required-fail` on the seeded orphan digest-differ), GREEN
  after (quorum passes, `[WARN] nornicdb_vs_neo4j_transient` names the
  statement).
- E2E on the attempt-2 failing capture
  (`differential-capture` artifact of run 35783305842,
  pair1/pair2 x nornicdb/neo4j), branch binary, quorum mode,
  `-diff-executions-advisory-max=200`:
  - stripped allowlist (no `transient_reads` section): exit 1,
    `[FAIL] nornicdb_vs_neo4j_quorum ... 1 reproduced divergence(s),
    first: MATCH (n:Module) ... row digest differs (nornicdb=427 rows,
    neo4j=406 rows)` — byte-identical to the CI failure.
  - committed allowlist: exit 0, `2 pass, 0 required-fail,
    2 advisory-warn`, transient WARN naming the Module page
    (2 excluded, one per pairing).
- Focused suites green: `backendconformance`, `graph/capture`,
  `golden-corpus-gate`; committed YAML parses with 49 entries + 2
  transient reads; gofumpt clean.

No-Regression Evidence: option-1 code runs only inside the gate's
offline backend-diff phase (same hot files as option 2, plus the new
finding line on the gate's stdout). The E2E pair above is the
same-binary before/after on the same capture: verdicts differ only as
designed (required-fail becomes transient WARN).

No-Observability-Change except one additive finding name:
`nornicdb_vs_neo4j_transient` (non-required WARN). No metric, span, log
key, or status field changes; existing finding and flag names unchanged.
