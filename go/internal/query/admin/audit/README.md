# Admin Audit Glue

Shared audit and permission helpers for the admin-handler family: the
`Appender` port the handlers' `Audit` fields use, the auth-to-audit-actor
mapping (including the shared-token synthetic identity), safe correlation
IDs, the permission-feature gate, local-identity hashes, and optional-time
row shaping.

Each helper cites the query-root source it was repointed from. The logic
stays canonical there and in the `queryauth` / `querycontract` leaves; this
package only re-sources it so the admin packages never import the query
root, which would cycle back through the root alias shim.
