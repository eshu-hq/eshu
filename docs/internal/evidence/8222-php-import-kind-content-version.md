# PHP use-import kind and the git content-version bump (#8222)

`phpImportKind` now reads the tree-sitter PHP grammar's `type` field instead of
the statement text, so `use Functional\Collection;` is a class import. That
changes the facts the git collector emits for unchanged input, so
`gitGenerationContentVersion` moves from `git-collector.v3` to
`git-collector.v4`. The bump is what makes already-indexed repositories store the
corrected facts; without it the store drops the new output as a duplicate.

## Performance Evidence

Performance Evidence: baseline is the fleet at `git-collector.v3`, where no
scope is pending a content-version re-collection. After the bump every git
scope's freshness hint changes, so each scope stores one new full generation at
its next forced reconcile or reindex. Backend is the git ingester and the
PostgreSQL store of the QA environment. Input shape is 812 active repository
scopes.

The re-collection path was measured on a scoped reindex of 69 repositories on
2026-10-16, which uses the same forced full snapshot and the same per-cycle cap
(`ESHU_REPO_RECONCILE_MAX_PER_CYCLE`, default 10, unset in the overlay):

| Window (UTC) | Repositories re-parsed |
| --- | --- |
| 10:50 to 10:59 | 10 |
| 11:00 to 11:09 | 10 |
| 11:10 to 11:19 | 10 |
| 11:20 to 11:29 | 20 |
| 11:30 to 11:39 | 0 |
| 11:40 onward | 7 so far (57 of 69 at 12:12) |

The pace is 7 to 10 repositories per 10 minutes (57 in 82 minutes overall),
bounded by the cap and by parse time; the 20 in one window shows it is not a hard
10. Extrapolation, not a measurement: 812 scopes at that pace is about 13 to 20
hours for the whole QA fleet to re-collect once. The production environment
pins `git-collector.v2`, so it moves v2 to v4 in a single roll and pays one
re-collection per scope, the same cost the v3 bump (#8225) accepted.

No query, lease, claim, queue or worker path changes. The parser change replaces
a lowercase-and-prefix check on the statement text with a field lookup on the
node; it was not benchmarked, and no allocation increase is expected.

NOT_CHECKED: a parser micro-benchmark; parse CPU and database load during the
fleet re-collection; the number of deployed imports with the affected namespace
shape (a replica scan was cancelled by a replication conflict).

## Observability

No-Observability-Change: this change adds no metric, span or log line. An
operator follows the re-collection with the existing `git_reconcile_forced` log
line (its `reason` field) and with the count of scope generations ingested after
the roll. The collector contract test `TestGenerationContentVersionPinsCollectorOutput`
pins the version against the emitted facts, so a future output change cannot skip
the bump silently.

## Known limit

A namespace whose first segment is exactly `Function` or `Const` (any case), such
as `use Function\Foo;`, is read by the vendored grammar as a kind keyword. That
limit pre-dates this change and is recorded in the PHP package-key evidence note
(`7601-php-package-keys.md`).
