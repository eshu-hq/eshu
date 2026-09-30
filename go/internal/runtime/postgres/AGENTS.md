# PostgreSQL reader access agent guidance

Read `README.md`, `config.go`, `physical.go`, `checkpoint.go`, `reader.go`,
`read_store.go`, and the parent
`go/internal/runtime/AGENTS.md` before modifying this package. `Config` must
use the parent's total pool defaults and preserve the collector writer path.
The private reader pool must remain read-only and never escape as `*sql.DB`.
All business SQL must be fenced on its exact borrowed connection. Keep writer
checkpoint acquisition after authorization and release it before waiting on
a reader. Cursor cleanup owns connection release on every terminal path.

The current scope allows native host candidates behind one writer pool and one
reader pool. Writer candidates must resolve to the frozen physical primary
incarnation; reader candidates must resolve to its streaming standbys, or to
the same primary when the DSNs are exactly equal. A new Access bootstrap is
required after writer restart. Promotion, proxies, Aurora, and split-brain
handling need separate proof. `ReadTransaction` owns its borrowed connection
until Commit, Rollback, or cancellation; a cursor only closes its own rows.
Snapshot cursor Scan must reject any `*sql.RawBytes` destination before touching
other destinations, close through its public Close, and preserve Close errors.
Use the owned disposable PostgreSQL fixture for physical replay tests; do not
point write tests at ops-qa. Coordinate fixture use with other agents. Run the
package tests with and without `-race`; classify a new `*_live_test.go` in the
live-test ledger in the same change.

Live candidate and restart tests require explicit owned fixture environment
variables documented in README.md. Never hardcode a session host, port, or
container target in committed tests. A restart test must first prove a fresh
Access is ready, then require the old Access to reject the new incarnation.
