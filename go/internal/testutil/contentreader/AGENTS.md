# testutil/contentreader — agent instructions

## Read first

- `doc.go` — what lives here.
- `driver.go` — the queue, the two-tier answer, and the default branches.
- `querytestutil/content`'s AGENTS.md — the adapter pattern for wide fakes,
  which applies if this driver's surface ever needs one.

## Invariants

1. Every helper lives in an ordinary `.go` file and is exported. A helper in a
   `_test.go` file here is unreachable from other packages' tests, which
   defeats the package's only purpose.
2. Test-only: no production file may import this package. A fake answers from
   values a test installs, so a production caller gets the zero value back, a
   silent wrong answer rather than a failure.
3. An empty queue with no matching default stays an error. Answering it with
   empty rows would let a handler issuing an undeclared read pass.

## Changing the driver

- A new relational read model gets a `Reader*Columns` helper plus a default
   branch in `defaults.go`, or undeclared reads of that shape fail. Match on
   the column set, not on the SQL text, so the test keeps choosing which read
   it is answering.
- Keep the queue FIFO and the assertions on the queued result: SQL text via
   `ReaderQueryContainsInOrder`, bind values via `ReaderCheckArgs`.
- Root's `openContentReaderTestDB` converts element by element through
   `shared()`; a field added to `ReaderQueryResult` needs a line there too.
