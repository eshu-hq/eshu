# #6782: execution-count divergences are advisory at the gate

Owner direction 2026-09-21: fix the differential oracle permanently instead
of excusing one flapping statement per PR.

## Symptom

After 574566079 retired allowlist entry 57, the `differential nornicdb vs
neo4j` job was red on every main run and on every open PR whose paths select
it, each run on a different statement:

| head | run | reproduced divergence (quorum) | kind |
| --- | --- | --- | --- |
| main 4c4bc378 | 35654686333 | stale allowlist entry 36 (fails before quorum) | parse |
| main 788edd169 | 35662995322 | workload-cloud USES read, `ORDER BY name, id LIMIT` | results |
| main 965631995 | 35664395755 | `UNWIND $pairs ... INVOKES_CLOUD_ACTION ... RUNS_IN` write, nornicdb=8 vs neo4j=7 | executions |
| PR #6892 9270f6dc | 35661069105 | USES read + package-consumption DEPENDS_ON MERGE, nornicdb=2 vs neo4j=3 | results + executions |
| PR #6931 | 35659894831 | USES read + `UNWIND $pairs` write | results + executions |
| PR #6932 995a98f9 | 35661933201 | `UNWIND $pairs` write | executions |

The USES read is a real non-total `ORDER BY` fixed in #6932. Every other
reproduced divergence is the `executions` kind: identical statement and
parameters, every record `Failed=false`, agreeing (empty) result sets, only
the number of executions differs. From the #6892 captures (artifact
10668492757), the DEPENDS_ON row `package-consumption:repository:r_ea78e8bb->
repository:r_3eddcea1` ran 3 times on Neo4j and 2 on NornicDB in both
pairings.

## Root cause

`CompareRecordings` defines the `executions` kind as scheduling noise (drain
passes, retries, regrouped batches). Quorum was expected to drop it as
pairing-local, but NornicDB and Neo4j drain the same intents at
systematically different speeds, so the pass-count difference reproduces
across both pairings. The only outlet was a per-statement `tier: executions`
allowlist entry; 21 had accumulated and each new corpus edge or drain loop
added another. `entryMatches` requires tier == kind, so the DEPENDS_ON
statement's existing `tier: missing` entry did not cover its `executions`
divergence either.

## Change

- `backendconformance.AdvisoryKind` / `SplitAdvisory`: `executions` is
  advisory; `missing`, `results`, `failures`, `rowcount` stay required.
- Quorum phase: reproduced `executions` divergences go to a new
  non-required finding `nornicdb_vs_neo4j_executions` (`[WARN]`, counted as
  advisory-warn, first statement named); `nornicdb_vs_neo4j_quorum` fails
  only on the required kinds.
- Single-pair `capture.Compare`: prints advisory lines and returns nil for
  executions-only divergences.
- Allowlist: the `executions` tier is removed from the parser (an entry
  using it is a parse error) and the 21 entries are deleted; the staleness
  exemption they needed is gone with them. 71 -> 50 entries, tiers now
  `missing` and `results`.

Row truth is unchanged: every read is still compared through `results` and
`missing`, and a one-sided error still fails through `failures`.

## Proof

Seeded RED/GREEN pairs, all run with `go test -count=1`:

- `TestSplitAdvisorySeparatesExecutions` (backendconformance): partition
  and order pinned.
- `TestCompareReportsExecutionsOnlyAsAdvisory` and
  `TestCompareStillFailsOnResultsWithExecutionsNoise` (capture): the
  executions-only pair returns nil and is named as advisory; a results
  divergence beside it still fails and is counted alone.
- `TestAllowlistRejectsExecutionsTier`,
  `TestAllowlistStatementTierMatchesAdvisoryKind`,
  `TestAllowlistTierScopesExcuseToKind` (now on `failures`).
- `TestRunBackendDiffQuorumReproducedExecutionsIsAdvisory` and
  `TestRunBackendDiffQuorumResultsBesideExecutionsFails`
  (golden-corpus-gate): the real phase over real sink files.

Shim, before and after, on the four real CI capture artifacts with the
committed allowlist (`golden-corpus-gate -phase=backend-diff` in quorum
mode; baseline binary built from main 965631995, after binary from this
branch):

| captures | before | after |
| --- | --- | --- |
| PR #6892 (35661069105) | 1 required-fail (2 reproduced: USES results + DEPENDS_ON executions) | 1 required-fail (USES results only), 67 executions advisory |
| main 965631995 (35664395755) | 1 required-fail (`UNWIND $pairs` executions) | 0 required-fail, 56 executions advisory |
| PR #6932 (35661933201) | 1 required-fail (`UNWIND $pairs` executions) | 0 required-fail, 70 executions advisory |
| PR #6931 (35659894831) | 1 required-fail (USES results + `UNWIND $pairs` executions) | 1 required-fail (USES results only), 56 executions advisory |

The two residual reds are the USES read, which #6932 fixes; main and #6932
themselves go green. The advisory counts are the noise the 21 entries had
been excusing plus the unexcused tail.

No-Observability-Change: no new metrics, spans, or log keys. The gate report
gains one finding line per quorum run.
