# testutil/contentreader

## Purpose

The shared fake `database/sql` driver for content-read tests, peeled out of
`querytestutil/content` for #6818 move 5. See `doc.go` for the godoc contract.

## Ownership boundary

Same as the other test-double leaves: helpers used by more than one package's
tests, and no production behavior. A helper used by one package belongs in that
package's own `_test.go` file. No production file may import this package.

## Exported surface

See `doc.go` for the godoc contract.

- `OpenReaderTestDB` / `ReaderQueryResult` — the shared fake `database/sql`
  driver. A test queues results; each query takes the head of the queue, runs
  that result's SQL-text and bind-value assertions, and answers with its rows.
- `ReaderQueryContainsInOrder` / `ReaderCheckArgs` — the same two assertions,
  callable directly for a test that holds a recorded query string rather than a
  queued result.
- `ReaderRelationshipReadModelColumns`, `ReaderDeploymentEvidenceColumns`,
  `ReaderRelationshipEvidenceColumns`, `ReaderDeadCodeCandidateColumns` — the
  column sets of four relational read models, used on both sides: a test
  declares one to say which read it is answering, and the driver answers that
  same read with the same helper when no result was queued.

### The driver's two-tier answer

The fake answers most queries from the queue, but a handler issues incidental
reads on the way to the query under test — a readiness probe, a language
rollup, a relationship count. Those are answered with an empty row set of the
right shape and leave the queue untouched; consuming the queue for them would
misalign every later expectation.

A test that genuinely asserts on one of those reads queues a result declaring
that read's own columns, and the queued rows then win. Matching on the column
set rather than the SQL text keeps the choice in the test's hands. An empty
queue with no matching default is an error, not an empty answer, so a handler
issuing a read nobody declared fails instead of passing.

### How root uses the driver without touching its callers

Root's `openContentReaderTestDB` keeps an unexported `contentReaderQueryResult`
struct with the original lowercase field names and converts the slice element
by element through `shared()` before delegating, so the queued-literal shape in
root's tests did not change with the move.
