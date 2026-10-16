# AGENTS.md — targeted backfill

Read the root and `backfill/AGENTS.md` first. Keep this package parent-free:
it may import `backfill` and lower-level contracts, never `postgres`.

Preserve exact shipped SQL derivations, batch sizing, worker count, lock order,
fan-in behavior, one reopen transaction, and all outcome and telemetry strings.
Parent callbacks must use the supplied database or transaction and must not
begin or commit their own transaction. Keep correlation domains root-owned.

Engine proofs belong here. Use real root entry points through external
`targeted_test` for whole-versus-scoped comparisons. Keep fixture SQL and
comparison logic in one parent-free implementation. Do not replace the whole
arm with a direct backfill engine call.

A hand-built `Store` must supply DB, Beginner, PhaseCommitted, GateReopen,
ReopenSucceeded, all three nonblank narrowed queries, and a nonempty list of
unique, nonblank correlation domains. The full pass rejects missing values
before span or database work. Standalone reopen needs Beginner, reopen
callbacks, queries, and domains; empty affected input returns before validation.
Keep the domain membership list in the root constructor, not in this child.
