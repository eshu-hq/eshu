# PostgreSQL reader access agent guidance

Read `README.md`, `config.go`, `checkpoint.go`, `reader.go`, and the parent
`go/internal/runtime/AGENTS.md` before modifying this package. `Config` must
use the parent's total pool defaults and preserve the collector writer path.
The private reader pool must remain read-only and never escape as `*sql.DB`.
All business SQL must be fenced on its exact borrowed connection. Keep writer
checkpoint acquisition after authorization and release it before waiting on
a reader. Cursor cleanup owns connection release on every terminal path.

The current scope is one static physical primary with one streaming standby,
or an exact same DSN assigned to both pools. An additional reader or writer
candidate, failover, Aurora, `QueryRow`, or transaction adapter needs a new
proof and contract.
Use the owned disposable PostgreSQL fixture for physical replay tests; do not
point write tests at ops-qa. Coordinate fixture use with other agents. Run the
package tests with and without `-race`; classify a new `*_live_test.go` in the
live-test ledger in the same change.
