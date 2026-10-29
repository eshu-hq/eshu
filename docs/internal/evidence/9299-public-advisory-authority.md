# Public advisory authority for derived image impact

The `supply_chain_impact_v1` public feed is an explicit, revisioned operator
declaration. A vulnerability-intelligence collector configuration lists its
ordinary `targets` and separately declares each public scope:

```json
{
  "targets": [{"scope_id": "vuln-intel://nvd/example", "source": "nvd"}],
  "public_advisory_targets": [
    {"scope_id": "vuln-intel://nvd/example", "max_age_seconds": 86400}
  ]
}
```

`max_age_seconds` is required and must be between 1 and 2,592,000. The database
matches each declared scope to exactly one configured target. It maps `nvd`,
`osv`, `cisa_kev`, and `first_epss` to fixed source classes and allowed fact
kinds. The collector payload cannot choose its own trusted source class or
content profile. Unknown and duplicate declarations fail the entire desired
revision.

The coordinator calls `reconcile_public_advisories_v1` with a server-owned
monotonic desired revision and the complete set of advisory-owned collector
instances. The protected function updates the collector row and registrations
in one transaction. A same-revision retry is idempotent only for the exact
same desired document. Conflicting or stale revisions fail. Adopted instance
identities remain protected after removal; legacy per-row coordinator writes
skip those rows while unrelated collector rows continue. Configuration,
enablement, declaration, producer grant, and approval changes use nonreused
database epochs. Removing a declaration revokes its registration immediately.

A dedicated typed-source-producer login receives a bootstrap-owner-installed
grant for one collector instance and advisory scope. The generic runtime writer
cannot stage or publish. The fact transaction first calls
`stage_public_advisory_generation_v1` before its workflow heartbeat or fact
write. The producer records each exact accepted fact batch through
`record_public_advisory_facts_v1` in that transaction. The state writer calls
`complete_public_advisory_generation_v1` only after all batches, checking the
protected membership against persisted JSONB, claim fence, and one complete
source-snapshot fact. Any mismatch rolls back the facts and receipt together.
An earlier generic generation cannot be approved retroactively.

The typed producer has a separate fixed ordinary-ingestion ACL profile. It can
read and heartbeat its workflow claim, read current tenant grants, upsert the
scope/generation/facts, enqueue the projector work, and refresh the three
accepted-fact side-key tables. A new scope's invoker trigger also needs
`SELECT, INSERT` on `projector_scope_claim_fences`. Migration 261 records each
required or forbidden operation for these tables; the policy installer grants
only the required operations to the producer login and revokes them on role
rotation. No ordinary sequence or additional application function grant is
needed for this commit path. The protected owner allocates scanner epochs;
the producer has no direct `USAGE`, `SELECT`, or `UPDATE` on those sequences.
The DBA must pregrant `USAGE` on the `public` schema if the database revoked
its default PUBLIC usage, and the connection attestation checks it.

After the fact transaction commits, a separate short transaction calls
`publish_public_advisory_generation_v1`. It checks the same current claim and
all pinned source authority, then moves the protected public activation pointer
and records an approved generation. Generic `ingestion_scopes.active_generation_id`
is only a projection; it cannot grant public authority. A duplicate completed
receipt cannot revive an older retired generation. A replaced or expired claim
requires a fresh generation identity. Approval expires at the committed
generation's `observed_at + max_age_seconds`; missing or expired approval is
not eligible for derived impact. No old generic advisory generation is
backfilled into public authority.

Lock order is role policy, desired policy, collector and epoch, registration,
producer grant, workflow claim, receipt, then fact rows or activation. Stage
and completion hold only database transactions; provider network work is
outside them. Registration CAS and producer stage/publish are separate
transactions with independent idempotency receipts. Current read paths must
check the protected activation, registration, producer grant, role/database
identity, expiry, and every fact's protected membership and JSONB hash before
applying limits or ranking findings.

Performance Evidence: The prior generic fact trigger theory added 6.0% and
6.6% to unrelated writes and was rejected. This typed path does not add a
generic fact trigger. Its post-integration admission and publication cost is
NOT_CHECKED.

Observability Evidence: The existing vulnerability-intelligence fetch and
fact emission signals identify provider work. Protected registration, stage,
completion, publication, conflict, expiry, and retry counters are NOT_CHECKED
until the typed producer route is wired. No production activation claim follows
from the SQL alone.
