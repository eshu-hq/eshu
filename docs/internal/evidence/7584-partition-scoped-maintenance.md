# Partition-Scoped Deferred Relationship Maintenance (#7584)

Proof-branch note for the D3 step 2 slice of #7584. The entry point
`IngestionStore.runDeferredRelationshipMaintenanceForPartitions`
(`go/internal/storage/postgres/ingestion_targeted_maintenance.go`) runs deferred
relationship maintenance for owed `(scope_id, generation_id)` partitions only.
Nothing calls it yet; the activation-obligation consumer will reach it through a
port.

## Contract

For every partition the pass touches, the committed
`relationship_evidence_facts` rows, `graph_projection_phase_state` rows,
`deferred_backfill_partition_memo` rows, and reopened `fact_work_items` equal
what `RunDeferredRelationshipMaintenance` produces for that partition. Outside
those partitions the pass changes nothing.

The touched set is computed, not assumed:

- Relationship resolution reads only the evidence attached to its own
  generation, and evidence is attached to the source repository's active
  generation. A change to an owed repository R therefore affects R's partition
  and the active partitions of the sources of evidence whose source or target is
  R.
- Sources are found with the target-catalog inbound loader
  (`loadAnchorScopedRelationshipFacts`, proven a superset in D3 step 1), grouped
  by fact partition. This finds other git repositories, GCP cloud scopes without
  a repository fact, and ArgoCD control repositories whose ApplicationSet reads
  an owed config repository.
- A source partition with no committed `backward_evidence_committed` phase has
  never been processed. It is promoted to owed so its own facts are loaded before
  its phase is published.
- An activation that changed the repository catalog fingerprint is refused with
  `errTargetedMaintenanceCatalogChanged`. A catalog change can alter evidence
  between repositories the owed partitions never touch.

The writes reuse the whole pass's own code: the memo-gated partition loader,
the batch writer (now taking its under-lock generation read as a parameter),
and the unchanged fan-in `publishDeferredBackfillPartition`. Every new query is
derived from the shipped query: one predicate inserted at a stable marker, or
the shipped query wrapped whole.
`TestTargetedMaintenanceQueriesDeriveFromShippedQueries` pins each derived
string byte for byte, and `TestBoundedRepositoryGenerationReadsMatchShippedRead`
pins the derived rows against the shipped rows, including two scopes that derive
the same repository id.

## Evidence

`TestTargetedMaintenanceMatchesWholePass` and
`TestTargetedMaintenanceInterleavingsMatchWholePass` seed two fully
bootstrapped schemas identically and run the real corpus-wide pre-pass to reach
steady state. They then activate the owed generation without a pass and run
the whole pass in one schema and the partition-scoped pass in the other. In the
touched partitions the rows must match with zero differences in both
directions. Outside them the partition-scoped schema must be unchanged. Each
fixture also states the evidence edges, phase rows and reopened work items it
expects. The fixtures cover a direct outgoing reference, inbound content, a
GCP relation (owed cloud scope, and inbound to an owed repository), an ArgoCD
ApplicationSet with an external config repository, two repositories in one
partition across separate batches, empty evidence, an unrelated quiet scope,
memo hit and miss, a generation advance before the evidence commit and before
the phase, successor activation, a failed sibling batch and its retry, an
unprocessed inbound source, and a catalog change.

## Performance and observability

No-Regression Evidence: the whole pass is unchanged at runtime. Its batch
writer now takes the under-lock generation read as a parameter, and the whole
pass passes `loadAllActiveRepositoryGenerations`, which calls the shipped
`loadActiveRepositoryGenerations` with the same query. The rest of the batch
transaction is the same code. `go test -p 2 -count=1 ./internal/storage/postgres`
passed on this branch (hermetic tests; live tests skip without a DSN). The
partition-scoped entry has no production caller yet, so no runtime path runs
it. Its own cost has not been measured; that is D3 step 3, and no claim is made
here.

Observability Evidence: the entry opens a `relationship.backfill_deferred_targeted`
span with owed, not-active, promoted, loaded, affected, evidence and published
counts. It logs one `deferred_backfill_targeted_completed` line with the same
counts, the relationship-domain reopen counts and the duration; the live
differential run emitted 15 of these lines. The reopen step records the existing
`DeploymentMappingReopened`, `CodeImportRepoEdgeReopened` and
`CorrelationReopened{domain}` counters, and the shared loader, batch and fan-in
code keep their existing `deferred_backfill_*` metrics and logs. No new
instrument was registered.

## Not covered

- Cost. No timing or row-count comparison at 900 scopes has run (D3 step 3).
- Concurrency of this entry against an ingester pass, consumer replicas, or
  lease expiry (D3 step 4).
- Graph and API truth after the reducer replays the reopened items.
- Correlation consumers whose dependency on the owed scope is not relationship
  evidence (for example a workload correlation waiting on an OCI scope). The
  whole pass reopens correlation items in every scope; this pass reopens them
  only in the touched partitions.
