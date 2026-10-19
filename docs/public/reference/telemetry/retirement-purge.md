# Retirement Purge Signals

The narrowed operator-driven repository retirement purge (#7766 increment
3) deletes a fenced repository's scope rows plus its scope- and repo-keyed
leftover rows, then completes the tombstone. These are its operator
signals, emitted from
`go/internal/storage/postgres/repository_retirements_purge.go` on the
reducer-owned runner.

| Signal | Type | Use |
| --- | --- | --- |
| `eshu_dp_repository_retirements_purged_total` | counter | Completed operator retirements, one per completed tombstone, unlabelled. Emitted after the purge commit, so a crash between commit and emission under-counts rather than double-counts. |
| `repository retirement purged` | INFO log | One line per purged repository (`repo_id`, `retirement_id`, `scopes_deleted`). |
| `repository retirement blocked: terminal generations not reached` | WARN log | The `terminal_not_reached` exit: a scope generation went live after the fence, so the purge wrote nothing and the fenced tombstone stays open. Carries `repo_id`, `retirement_id`, `scope_ids`, and `blocking_statuses` read back from the terminal gate (for example `active:1`). Only a new fence can move the blocking generation. |
