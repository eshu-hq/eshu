# Governance audit identity

## Purpose

`governance/audit` renders the redacted identity fields shared by every
governance audit event the coordinator emits: the scope-id hash, the
correlation id, the emitting service principal, and the timeout that bounds one
append.

It exists because two unrelated emitters need the same shaping. The coordinator
root emits collector-egress and extension-egress denials from
`governance_audit.go`; `coordinator/semantic` emits semantic-provider egress
decisions from its claim loop. A subpackage cannot import the root, because the
root imports the subpackage, so the shared half was hoisted here rather than
duplicated or exported across the two.

## Ownership boundary

This package owns identity rendering and the append timeout value. It does not
own an appender, an event type, an event's semantics, or the decision that
produced it. `internal/governanceaudit` owns the `Event` shape, enums, and
normalization. Each emitter owns which event type, actor class, scope class,
decision, and reason code it sends, and owns the interface it appends through.

## Exported surface

- `Hash(parts ...string) string` renders the validation-safe scope-id hash.
- `CorrelationID(prefix string, parts ...string) string` renders the
  correlation id for the events one decision emits.
- `ServiceID` is `svc:workflow-coordinator`.
- `AppendTimeout` is 500ms.

See `doc.go` for the godoc contract.

## Dependencies

Standard library only: `crypto/sha256`, `encoding/hex`, `strings`, `time`. The
package imports neither the coordinator root nor `internal/governanceaudit`.

## Telemetry

None. This package registers no instrument and emits no log. The events whose
identity it renders are appended by its callers and surface through the
governance audit store.

No-Observability-Change: extracting these helpers from the coordinator root
adds or renames no metric, span, log field, status field, queue, worker, lease,
or runtime setting. `Hash` and `CorrelationID` are byte-identical to the root's
former `governanceAuditHash` and `governanceAuditCorrelation`, so every audit
row's `scope_id_hash` and `correlation_id` value is unchanged.

## Gotchas / invariants

- `Hash` joins its parts with a NUL separator, not a delimiter that can appear
  in a part. Joining with `:` or `/` would let one part's content forge a part
  boundary and collide two different scopes onto one hash. Do not change the
  separator.
- The `sha256:` prefix is part of the stored value, not decoration. It names
  the digest algorithm so a future migration can tell old rows from new ones.
  `CorrelationID` strips it deliberately before truncating.
- `CorrelationID` truncates to the leading 16 hex characters, 64 bits. That is
  a correlation handle for joining one decision's events, not a collision-proof
  identity. Do not reuse it as a primary key.
- Parts are trimmed but not lowercased or otherwise normalized, so a caller
  that varies the case of a part produces a different hash. Callers pass
  already-normalized collector kinds and component ids.
- Changing `Hash`, its separator, its prefix, or the truncation length
  invalidates every previously stored hash and correlation id. Existing rows
  are not rewritten, so a change silently splits one logical scope into a
  before-and-after pair.
- `AppendTimeout` is deliberately short. The audit append sits inline on the
  reconcile and claim paths, so the timeout is what keeps a wedged audit store
  from stalling scheduling.

## Related docs

- `go/internal/coordinator/README.md`
- `go/internal/coordinator/semantic/README.md`
- `go/internal/governanceaudit/README.md`
