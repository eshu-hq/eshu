# Evidence: sweep lease TTL covers the graph write budget (#7047)

## Change

The graph orphan sweep (`graph_orphan_sweep` partition lease) and the
value-flow stale cleanup (`code_value_flow_stale_cleanup` partition lease)
claimed 5-minute leases with no heartbeat and no startup check. On ops-qa
`ESHU_CANONICAL_WRITE_TIMEOUT` is 300s, so a write running to its full budget
reached the end of the lease with no margin and a second holder could claim
mid-cycle (the store grants a different owner once `lease_expires_at` passes).
Arbiter verdict for #7047: LENGTHEN_TTL_ONLY (a boot-time gate would fail
ops-qa at startup; heartbeat machinery deferred — the renewal precedent is
`ProcessPartitionOnce`'s TTL/2 same-owner re-claim from #4449).

Default TTLs 5m to 10m in four places: `maintenance`
`defaultGraphOrphanSweepLeaseTTL`, `cleanup` `defaultLeaseTTL`, and the two
`cmd/reducer` loader defaults that mirror them. Registry defaults and the
reducer README updated to match. Explicitly configured TTLs still pass
through verbatim (overrides tests unchanged); only the unset default moved.
No cycle, claim, release, or batching logic changed.

## No-Regression Evidence (#7047):

- Conflict domain: the two single-partition sweep leases above; worker
  settings unchanged (hourly polls, orphan batch 100 / count 10k, cleanup
  100 scopes x 500 deletes).
- Inequality: effective default 600s > 300s write budget + 30s margin (margin
  mirrors `repoDependencyProjectionLeaseSafetyMargin`).
- New contract tests `TestGraphOrphanSweepDefaultLeaseTTLCoversWriteBudget`
  and `TestStaleCleanupDefaultLeaseTTLCoversWriteBudget` failed before the
  change (300s <= 330s) and pass after.
- Full packages green: `internal/reducer/maintenance`,
  `internal/reducer/code/value/cleanup`, `internal/envregistry` (doc
  regenerated), `cmd/reducer` (two defaults-contract tests updated 5m to 10m
  for the intended default move; 90s/2m overrides passthrough unchanged).
- Live Postgres 18 (lane-N-pg) mechanism proofs green: claim binds TTL for
  server-side expiry, blocked claim keeps full TTL, same-owner renewal keeps
  the lease alive against a rival, release clears the row — the lengthened
  TTL extends the same proven mechanism, it does not alter interleaving.

## Observability Evidence (#7047):

- Both cycle-completed and cycle-failed logs now carry `lease_ttl_seconds`
  (the configured TTL for that cycle, emitted via the shared
  `telemetry.LogKeyLeaseTTLSeconds` constant registered in
  `telemetry.LogKeys()` and pinned by structured-log capture tests on both
  runners). On the completed log it sits next
  to the existing `lease_acquired` flag, so an operator can see which TTL
  was claimed at the start of a finished cycle (no renewal: compare with
  `duration_seconds` before inferring ownership lasted through
  completion). On a failed cycle the field is the TTL that
  would have guarded the cycle, not proof one did: `recordFailure` also
  runs when the lease claim itself errors (`claim ... lease`), before any
  lease is held, and the failure log carries no `lease_acquired` flag —
  so pair a failure log with its error message before inferring anything
  about expiry. Failure classes unchanged (`graph_orphan_sweep_error`,
  `code_value_flow_stale_cleanup_error`).
