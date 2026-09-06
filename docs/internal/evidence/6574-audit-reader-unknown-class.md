# #6574: the governance-audit reader keeps a class it does not know

Issue #6574. Branch `claude/6574-audit-reader`. Follow-up to
`6459-browser-session-actor-class.md`, whose rolling-upgrade caveat this
change retires for future class additions.

## What was wrong

`scanGovernanceAuditEvent` in `go/internal/storage/postgres` ran every stored
row through `governanceaudit.NormalizeEvent`, the write-path validator whose
`type`, `actor_class`, `scope_class`, and `decision` enums are closed. A row
whose class the running binary did not know failed the whole `List` call, so
`GET /api/v0/auth/admin/audit/events` answered 500 for any page holding it.

That is a rolling-upgrade problem: when a release adds a class, a new pod
writes it while an old pod is still serving reads. #6573 added
`browser_session` and hit exactly this. The row itself was persisted
correctly; only the read failed, and only until every pod was on the new
build. It would recur on the next class addition.

## What changed

`governanceaudit` now has one normalizer with two enum policies:

- `NormalizeEvent` stays the write-path contract. `Append` still rejects an
  unknown class, so a producer on this build cannot emit one.
- `NormalizeStoredEvent` is the read-path contract. It keeps a `type`,
  `actor_class`, `scope_class`, or `decision` this build does not know,
  verbatim, when it is 1-64 bytes of `[a-z0-9_]` (the shape every registry
  constant has), and rejects anything else. Trimming, the hash and token
  guards, the reason-code guard, and the `occurred_at` guard run unchanged.
  The actor-identity rule applies only to a class this build knows; the
  build that accepted an unknown class is the one that knows whether it
  carries an identity.

`scanGovernanceAuditEvent` uses `NormalizeStoredEvent`. `Aggregate` does too,
so the in-memory summary counts an unknown class under its stored name, which
is what the SQL `Summary` already did (it groups the column as a plain string
and `applyGovernanceAuditSummaryRow` appends whatever name it gets).

No sentinel such as `unknown` is introduced; the operator sees the value the
writer stored, which is the value they will filter by once their pod is
upgraded.

## Evidence

The regression test `TestGovernanceAuditStoreListKeepsUnknownEnumValuesVerbatim`
(`go/internal/storage/postgres`) drives the production `List` path over a fake
row with `actor_class = future_class` (and one case each for `event_type`,
`scope_class`, `decision`). Before the fix:

```text
--- FAIL: TestGovernanceAuditStoreListKeepsUnknownEnumValuesVerbatim/actor_class
    List error = governance audit field "actor_class" is invalid, want nil
```

After the fix the four cases pass and the row comes back with the stored
value. `TestGovernanceAuditStoreListStillRejectsUnsafeStoredRows` and
`TestNormalizeStoredEventStillRejectsUnsafeValues` pin what the tolerant reader
does not loosen (a non-token class, a raw email in a hash field, a URL in a
correlation id). `TestGovernanceAuditStoreAppendRejectsUnknownActorClass` and
`TestNormalizeStoredEventKeepsUnknownEnumsThatWriteRejects` pin the write side
staying closed. `TestAggregateCountsUnknownClassAsPlainString` covers the
in-memory summary.

No-Regression Evidence: the scanner still runs one normalizer call per row
with the same trim and guard checks; the only added work is a byte scan of an
enum value that is at most 64 bytes, and it runs only when the value is
outside the registry. No SQL text, predicate, index, lease, worker, batch, or
transaction boundary changes. `governance_audit_events` is unchanged; the
`actor_class` column was already unconstrained `TEXT`.

No-Observability-Change: no metric, span, log scope, or status field is added
or removed. The operator-visible change is that the audit-list page no longer
returns 500 for a row with an unfamiliar class; the row appears with its
stored value.

Concurrency: `List` is a read-only `SELECT` with no lock, claim, or lease. The
only conflict domain is the rolling-upgrade interleaving itself (new pod
writes, old pod reads), which this change makes safe by construction rather
than by ordering the rollout.

## Rollout window

The release that introduced `browser_session` still has the window, because
its old pods ship the strict reader. From this change on, an old pod reads a
newer pod's rows, so a later class addition has no window.
