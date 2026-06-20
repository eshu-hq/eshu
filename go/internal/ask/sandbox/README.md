# ask/sandbox — Tier 2 Read-Only Query Sandbox

The `sandbox` package implements a **Tier 2, default-off, default-deny** read-only
query sandbox for the Ask Eshu feature. It gates Cypher and SQL queries through
three ordered security layers before any execution attempt reaches a backend.

## Status

**DEFAULT-OFF.** The Guard refuses every query until `enabled=true` is passed at
construction. Enabling requires the security review tracked in issues **#1755**,
**#1900**, and **#1902** to be completed and signed off. Do not enable the Guard
in any production path until that review is closed.

## Security Architecture (3 layers)

```
caller query
   │
   ▼
┌─────────────────────────────────────────────────────┐
│  Layer 1 — normalize (normalize.go)                 │
│  • Adversary-resistant left-to-right byte scanner.  │
│  • Masks comment content (-- / // / /* */),         │
│    string literals ('' / $tag$ / "…" / `…`),       │
│    and their delimiters with space characters.      │
│  • Rejects control bytes (< 0x20 except TAB/LF/CR, │
│    and DEL 0x7F) that can split keyword tokens.     │
│  • Counts non-empty statements separated by `;`.   │
└─────────────────────────────────────────────────────┘
   │  normalized.masked, normalized.statementCount
   ▼
┌─────────────────────────────────────────────────────┐
│  Layer 2 — Validate (guard.go, cypher.go, sql.go)  │
│  • Length check: len(query) > caps.MaxQueryLen.     │
│  • Dialect dispatch: Cypher or SQL only.            │
│  • Exactly one statement (no stacked statements).   │
│  • Cypher: RETURN required; write/DDL denylist      │
│    (CREATE MERGE DELETE SET REMOVE DROP FOREACH     │
│    CALL LOAD DETACH); whole-word token matching so  │
│    relationship type :CALLS != keyword CALL.        │
│  • SQL: must start with SELECT or WITH; write/DDL/  │
│    dangerous-function denylist (INSERT UPDATE       │
│    DELETE TRUNCATE MERGE CREATE ALTER DROP GRANT    │
│    REVOKE COPY CALL DO SET VACUUM ANALYZE REINDEX   │
│    LOCK BEGIN COMMIT ROLLBACK INTO PG_SLEEP         │
│    PG_TERMINATE_BACKEND DBLINK PG_READ_FILE         │
│    LO_IMPORT LO_EXPORT NEXTVAL SETVAL LO_UNLINK     │
│    LO_CREATE PG_ADVISORY_LOCK and variants).        │
│  • Deny reasons are BOUNDED: they never echo the    │
│    query body or reveal secrets.                    │
└─────────────────────────────────────────────────────┘
   │  Decision{Allowed:true}
   ▼
┌─────────────────────────────────────────────────────┐
│  Layer 3.5 — Cost gate (costgate.go)                │
│  Active when Caps.MaxPlanCost > 0, or               │
│  Caps.MaxEstimatedRows > 0, or ForbiddenPlanOps≠∅. │
│  • Runs EXPLAIN (FORMAT JSON) inside the same       │
│    read-only context (never a real execution).      │
│  • Rejects if Total Cost > Caps.MaxPlanCost.        │
│  • Rejects if Plan Rows > Caps.MaxEstimatedRows.    │
│  • Rejects if any forbidden operator is found in    │
│    the plan tree (e.g. "Seq Scan").                 │
│  • Fail-closed: explainer error → rejection.        │
│  • Cypher v1: passes straight to Layer 3.           │
└─────────────────────────────────────────────────────┘
   │  Decision{Allowed:true, plan in budget}
   ▼
┌─────────────────────────────────────────────────────┐
│  Layer 3 — Guarded read-only-tx exec (pgexec.go)   │
│  • SQL: BeginTx with ReadOnly:true (database-level  │
│    enforcement as defense-in-depth after layers 1   │
│    and 2). context.WithTimeout(caps.Timeout).       │
│    Scans at most caps.MaxRows rows; truncates.      │
│    Rolls back unconditionally (no writes committed).│
│  • Cypher v1: returns an error immediately; Cypher  │
│    graph backend execution is not wired in v1.      │
└─────────────────────────────────────────────────────┘
```

### Adversarial findings closed before merge

- **Dollar-quote stacked-statement bypass**: `$$; DELETE FROM …$$` was
  misidentified as a dollar-quoted string and masked the `DELETE`, hiding the
  stacked statement from the statement counter. Fixed by the dollar-quote tag
  scanner in normalize.go (digit-leading `$N$` treated as a positional parameter,
  not a quote opener).
- **SELECT INTO table bypass**: `SELECT * INTO new_table FROM …` creates a table
  inside an otherwise SELECT-shaped query. Fixed by adding `INTO` to sqlDenylist.
- **Sequence-mutation bypass**: `SELECT nextval('seq')` and `setval('seq',…)` are
  write side-effects callable from a SELECT. Fixed by adding `NEXTVAL` and `SETVAL`
  to sqlDenylist.
- **Advisory-lock DoS bypass**: `SELECT pg_advisory_lock(…)` acquires a session
  lock without writing data. Fixed by adding all four advisory lock function
  variants to sqlDenylist.

## Default Deny

Every uncertain or unclassified case results in a **deny**. The Guard never
falls through to execution on an ambiguous input.

## Leak-Safe Reasons

`Decision.Reason` is always a bounded, low-cardinality string. It never echoes
the query body and never reveals secrets, schema names, or internal state. This
makes it safe to return directly to the caller for logging and user display.

## Resource Caps

`Caps` controls four limits:

| Field             | Default   | Purpose                                                   |
|-------------------|-----------|-----------------------------------------------------------|
| MaxRows           | 1000      | Truncate result sets to prevent memory bloat.             |
| MaxBytes          | 1 MiB     | Byte budget for results (wired by API layer).             |
| Timeout           | 5s        | Cancel long-running queries.                              |
| MaxQueryLen       | 8192 B    | Reject oversized query strings up-front.                  |
| MaxPlanCost       | 1000.0    | Reject plans whose planner cost estimate exceeds budget.  |
| MaxEstimatedRows  | 100,000   | Reject plans whose row estimate exceeds budget.           |

`NewGuard` with a zero `Caps` automatically promotes to `DefaultCaps()`.

## Non-Goals (v1)

- **Tenant scope-predicate injection**: ensuring that a query only returns rows
  scoped to the authenticated tenant is the responsibility of the API layer
  (issue #3263). The Guard enforces read-only safety; it does not add `WHERE
  tenant_id = ?` predicates. This is the primary unsolved security problem that
  blocks Tier-2 enablement; see the security-review design package #3302.
- **Cypher execution**: Cypher graph queries against NornicDB require a graph
  backend client that is not wired in v1. `NewPostgresReadOnlyExecutor` returns
  an error immediately for `DialectCypher`.
- **Cypher cost gate**: `CostGateExecutor` passes Cypher queries directly to the
  inner Executor; EXPLAIN-based cost gating for NornicDB Cypher is deferred.

## Usage

```go
// Construct once, keep alive for the lifetime of the service.
// enabled=false until the security review (#1755/#1900/#1902) is complete.
guard := sandbox.NewGuard(
    sandbox.NewPostgresReadOnlyExecutor(db),
    sandbox.DefaultCaps(),
    false, // DEFAULT-OFF
)

// At the API handler:
decision, rows, err := guard.Run(ctx, sandbox.DialectSQL, query)
if err != nil {
    // sandbox disabled or execution error
}
if !decision.Allowed {
    // return decision.Reason to the caller (bounded, leak-safe)
}
```

## Related Issues

- **#3250** — Ask Eshu Tier 2 epic
- **#3261** — This package (sandbox)
- **#3263** — API layer: scope-predicate injection (still open)
- **#3302** — Tier-2 enablement gate; cost gating now lives in this package
  (EXPLAIN in the same read-only tx as execution), per its design doc
- **#1755**, **#1900**, **#1902** — Security review gate (required before enabling)
