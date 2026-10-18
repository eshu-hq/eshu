# 6818 residual: querycontract stays (arbiter ruling, 2026-10-18)

Five of the six renames in #6818 landed as #6846, #7027, #7034, #7039,
#7049, and #7078; the move proof stays in 6818-query-package-moves.md.
What remains is `go/internal/query/querycontract` with 41 top-level
non-test files, imported by 1,450 Go files. Its planned target collides
with the existing `go/internal/query/contract`, which holds 40 files and
38 `init()` capability rows. The drain vehicle #6642 closed on 2026-10-17
with the drain unexecuted. Counts measured at origin/main 19f7143e3.

The two packages hold different things: shared vocabulary and sentinels
versus capability rows. Renaming now conflates two responsibilities and
adds a self-import, so accuracy forbids it. A standalone drain only to
free a directory name inverts the Accuracy-first priority, and no
rule-clean interim name exists. The legacy stutter blocks nothing
(naming.md gates only Added/Renamed paths), so the residual is tracked,
not hidden.

## Trigger

Follow-up #8490 runs when `query/contract/` holds at most 3 non-test
files via family-owned registrations, or when an explicit standalone-drain
decision lands on that issue. No rename and no standalone drain now.

## Proof required when the follow-up runs

Prior-move pattern: byte-substitution proof per moved file, test-name
union equality, assembled-registry row census before and after,
moved-file-refs and package-docs gates, and the sentinel-identity note in
`go/internal/query/querycontract/doc.go`.
