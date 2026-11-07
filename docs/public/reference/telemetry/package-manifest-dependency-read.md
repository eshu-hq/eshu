# Package Manifest Dependency Read Signals

A package-registry reducer intent loads the active Git manifest dependency
rows for its ecosystem and package names through
`ListActivePackageManifestDependencyFacts`, 500 rows per keyset page. These
signals cover one whole read, across all of its pages (#9586).

The `read` label names the query text that ran: `plain` for every ecosystem
except Maven, `maven_family` for the Maven query that also reaches Gradle and
sbt rows. `unclassified` means a query text shipped without a label; a test
refuses that, so seeing it is a bug to report. Only completed reads record a
sample; a failed read surfaces as the reducer intent's failure.

- `eshu_dp_package_manifest_dependency_read_seconds`: histogram of one read's
  wall time, labeled `read`.
- `eshu_dp_package_manifest_dependency_read_pages`: histogram of the keyset
  pages one read issued, labeled `read`. It counts the active query's pages
  only; the delta-base pages show in the wall time.
- `eshu_dp_package_manifest_dependency_read_delta_union_total`: counter of
  delta-base union decisions, one per delta-active scope a read reached,
  labeled `read` and `outcome` (#9991). `union_base_found` added the base
  pins the delta did not override, delete, or tombstone.
  `union_base_unmatched` resolved a base that holds no pin for the read's
  names, or one that retention drained without recording an event.
  `union_base_missing` (the #7797 rule refused the base: never projected,
  retention-pruned, or the prune probe failed), `union_scope_mismatch` (no
  `delta_generation` repository fact), and `union_no_baseline` (a legacy
  delta) return that scope's delta rows only. `union_base_missing` and
  `union_scope_mismatch` also log a WARN line with the scope, generation,
  and both commits, from the reducer's manifest reads and the supply-chain
  impact read alike. `union_resolve_failed` counts once per read whose
  target lookup, base page, base resolution, or overlay read failed; the
  whole read returned the error.

A delta-active repository reads its delta overlaid on its base full
generation, the same base the projector overlays
([Delta-base union](../../operate/graph-delta-union.md)). A rising
`union_base_missing` means those repositories lose their unchanged pins
until the forced full re-parse that the projector's own
`refused_base_missing` arms. Wall time that rises with
`union_base_found` is the cost of reading the base pins.

Joining a refusal to security-alert reconciliation (#10193): that consumer
stays on the delta-only fallback (upsert-only writer, no retract), so a
`union_base_missing` or `union_scope_mismatch` decision can make it commit
`provider_only` for an alert whose pin exists only in the refused base.
Find the refusal on this counter and its WARN line (`scope_id`,
`generation_id`, `delta_baseline_commit_sha`,
`delta_union_base_commit_sha`), then join to
`reducer_security_alert_reconciliation` facts with
`reconciliation_status = provider_only` for the same repository committed
after the refusal; the handler success line's `provider_only=<n>` evidence
summary is the second key. The reconciliation emits no refusal signal of
its own — that absence beside a post-refusal `provider_only` is the
signature. The alert expression and the re-drive steps are in
[Security-alert reconciliation on a refused base](../../operate/graph-delta-union.md#security-alert-reconciliation-on-a-refused-base-10193).

Read them together. Wall time that grows with pages is the expected cost of a
large name set. Wall time that climbs while pages hold means each page got
slower. That is the shape #9586 fixed: once PostgreSQL switched a cached
statement to its generic plan, each page cost seconds even with no rows to
return. Each page now runs on the unnamed statement with a custom plan.
