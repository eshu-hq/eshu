# AppSync API key disclosure assessment (#8713)

Alert #20 identified AppSync API key values flowing into durable resource facts.
AWS uses the `ApiKey.Id` value as the key clients send in `x-api-key`; it is a
bearer credential, despite the SDK field name. The earlier adapter copied that
value into each key resource's identity, name, `key_id` attribute, correlation
anchors, and source record ID.

The fix drops the SDK `Id` and free-text description before scanner-owned state.
The API resource retains only the observed key count and ordered expiry/deletion
timestamps. It emits no per-key graph resource. Tests cover the SDK snapshot,
serialized fact envelopes, the Postgres cloud-inventory allowlist, and the HTTP
readback projection.

The source fix does not erase historical records. Fact retention prunes eligible
superseded generations under its configured age and row limits; it is not an
immediate targeted secret purge. A fresh successful AppSync scan may remove
predecessor-only graph resource nodes through the existing generation-aware
materialization path, subject to that path's runtime configuration. Neither
production fact records nor deployed graph state were inspected or changed here.

Before treating deployed exposure as resolved, rotate AppSync API keys for the
scanned API scopes that used the vulnerable collector, then have the data owner
assess historical fact and graph cleanup for those exact scopes. Any targeted
production deletion needs its own reviewed procedure; this PR does not execute
one.

**NOT_CHECKED:** deployed API scope coverage, historical persisted values,
current graph nodes, key rotation, and targeted cleanup.
