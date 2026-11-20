# Schema comment revert: no-regression record (drive/11038)

Follow-up to #10990: that PR's comment-only pointer drops touched two
methodology-pinned production sources
(`go/internal/graph/schema_tables.go`,
`go/internal/graph/schema_tables_indexes.go`), whose byte identity the
methodology test compares outside issue #9303's declaration. This PR
reverts exactly those hunks.

No-Regression Evidence: `git diff` of both files against the pre-lane
base is empty after this revert; every hunk is a `//` comment line, so
no statement, expression, index definition, or string literal changes.
The methodology identity test compares these exact bytes in CI.

No-Observability-Change: no log, metric, span, status, or dashboard
line is added, removed, or altered.
