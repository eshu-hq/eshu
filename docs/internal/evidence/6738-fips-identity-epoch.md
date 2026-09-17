# Ops-QA FIPS identity epoch probe

Root-Cause Evidence: During the #6738 scratch-reset rollout on 2026-09-17,
`container_image_identity` reducer work entered `dead_letter` with
`probe identity epoch: ERROR: could not compute MD5 hash: unsupported`.
The ops-qa PostgreSQL 18.3 container has `OPENSSL_FIPS=yes`. A read-only
`SELECT md5('foo')` returned that same error; built-in SHA-256 returned the
expected digest. The production `probeIdentityEpochQuery` called `md5()` on
the active-generation mapping before loading identity facts, so the reducer
could not reach its decision or graph-write stages.

The replacement hashes the same ordered `scope_id:active_generation_id`
mapping, with the same empty-set handling, using PostgreSQL's built-in
SHA-256 over UTF-8 bytes. The epoch is process-local; a restarted reducer
does not compare a new fingerprint with an older binary's cached digest.
No cache lock, load, retry, or publication boundary changes.

No-Regression Evidence: A read-only `EXPLAIN (ANALYZE, BUFFERS)` of the
replacement aggregate on the live ops-qa corpus read 808 scopes in 2.135 ms
with 797 shared-buffer hits. The old MD5 expression cannot complete on this
FIPS server, so these values are not a before/after speed comparison.
`TestIdentityEpochProbeOnFIPSPostgresLive` called the production probe over a
read-only port forward: it failed on the old query with the observed MD5
error and passed after the SHA-256 change. The existing ordered-map shape
guard and supersession contract remain in the focused storage suite.

Observability Evidence: The existing
`eshu_dp_identity_cache_probe_duration_seconds` histogram still records the
epoch probe, and reducer failures retain their domain and failure-class logs
plus durable `fact_work_items` status. No new metric, span, or label is needed
for this hash substitution.
