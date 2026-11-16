# A rejected-only product finding lends no severity (#10764)

`severityObservation` (`go/internal/reducer/supplychain/core/product.go`) picks
the record a product-keyed (CPE) finding reads its CVSS from. It now reduces the
group to the newest copy of each record first, skips retracted records, and
blanks the score, vector, and label when every record is retracted. The package
path already behaved this way. This note records the cost and how an operator
sees the change. The decision is in the PR and in
`go/internal/reducer/supplychain/core/README.md`.

## Cost

Performance Evidence: measured with a scratch micro-benchmark (not committed) that calls
`severityObservation` on one group, run on `origin/main`'s `product.go` and on
this branch's, `go test -bench -benchmem -count=5`, Go 1.26.11, an 18-thread
Apple Silicon laptop. Input shape: n records spread over three sources
(`nvd`, `osv`, `cve_list`), each with its own advisory id and a source-reported
score, every tenth one Rejected, so no record has two copies. No database is
involved; the function reads the group already in memory.

| records in the group | main | this branch |
| --- | --- | --- |
| 1 | 80 ns, 0 allocs | 82 ns, 0 allocs |
| 4 | 198 ns, 0 allocs | 211 ns, 0 allocs |
| 32 | 1.4 us, 24 B, 3 allocs | 2.5 us, 2.8 KB, 6 allocs |
| 256 | 11.6 us, 200 B, 25 allocs | 19.1 us, 22 KB, 28 allocs |

Groups of up to four records take the allocation-free pairwise duplicate check
in `hasDuplicateCopyKey`; larger groups build one key set. The run measured no
real group-size distribution, so how many production groups exceed four records
is not known; a group holds one record per source plus any copies of a record
from other scopes, and the collectors feed four sources.

The committed `BenchmarkProductSeverityObservation` covers the copy-heavy shape
(copies of one record). Score-last with 8 copies measured about 700 ns and 576 B
on this branch against about 410 ns and no allocation on `main`; with 64 copies
about 5.1 us and 11.4 KB against about 3.0 us. The vector-only shape is faster
here, about 1.8 us against 4.4 us at 8 copies and 6.4 us against 35 us at 64.
These figures came from one review run and are not from the same machine state
as the table above. The extra cost is once per finding group per pass.

## Safety

No-Regression Evidence: the change is an in-memory reduction and two retraction checks inside a
function that already loops over the group. It adds no query, join, index,
lock, queue write, or concurrency. Finding identity, the stable fact key, and
the finding id do not move (they are the representative's and the writer's
identity code does not read the evidence list), so winner selection and keyset
paging see the same keys. The evidence list holds the representative when every
record is retracted, and can name the newest copy when a record has two copies,
so the writer's change detection can see a different list for such a group.
Only `cvss_score`, `cvss_vector`, `severity_label` and the values derived from
them can move, and only for a group whose records are retracted or have a
superseded copy. The reducer contention and winners live tests did not change.

## Operator signal

Observability Evidence: no new metric, span, or log key. An operator reads the effect through the
existing `eshu_dp_supply_chain_severity_decisions_total{outcome}`: a rejected-only
product finding moves from `source_reported` (or `derived_from_vector`,
`label_only`) to `none`, so the `none` count can step up on rollout and
settles after the next pass re-derives each scope. Existing rows pick up the new
score only when their scope re-derives. A finding's retraction annotation
(`advisory_retracted` in `priority_reason_codes`) is unchanged.

## Not checked

- The real distribution of group sizes in a QA-sized corpus.
- The size of the `none` step on a real corpus; the fixture shows one finding
  moving from score 100 to 60.
