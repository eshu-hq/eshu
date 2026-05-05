# AGENTS.md — internal/workflow guidance for LLM assistants

## Read first

1. `go/internal/workflow/README.md` — ownership boundary, type inventory, and
   key invariants
2. `go/internal/workflow/types.go` — `Run`, `WorkItem`, `Claim`, and all status
   enums; understand the fencing token pattern before touching claim types
3. `go/internal/workflow/store.go` — `ControlStore` interface; every method
   signature is a durable contract implemented by `storage/postgres`
4. `go/internal/workflow/progress.go` — `ReconcileRunProgress`; understand the
   status transition table before adding or changing `RunStatus` values
5. `go/internal/workflow/collector_contract.go` — the `collectorContracts` map
   and `RequiredPhasesForCollector`; the coordinator and storage layers both
   consume this lookup
6. `go/internal/workflow/fairness.go` — `FamilyFairnessScheduler` and its
   internal weight mutation; understand the single-use constraint before passing
   it across goroutines

## Invariants this package enforces

- **All Validate methods are strict** — non-blank identifiers, known enum
  values, and `updated_at >= created_at` are required. Do not add fallback
  behavior that silently ignores invalid rows.
- **ReconcileRunProgress is pure** — it takes a snapshot and returns new
  values without side effects. Do not add Store calls or mutations inside it.
- **FamilyFairnessScheduler is not safe for concurrent use** — `Next` mutates
  `currentWeight` on each call. External locking is required if multiple
  goroutines share one instance.
- **ControlStore interface is the Postgres contract** — every method in
  `ControlStore` has a corresponding implementation in
  the Postgres WorkflowControlStore implementation. Changing method
  signatures requires a coordinated storage update.
- **collectorContracts lookup must be exhaustive** — unknown collector kinds
  return an empty phase list; `ReconcileRunProgress` will treat them as having
  no requirements, which may silently permit premature `complete` transitions.

## Common changes and how to scope them

- **Add a new collector family** → add an entry to `collectorContracts` in
  `collector_contract.go` with the correct `CollectorKind`, `CanonicalKeyspaces`,
  and `RequiredPhases` slice; add a test in `collector_contract_test.go`; verify
  that `ReconcileRunProgress` transitions work correctly with the new phases
  in `progress_test.go`. No changes to `coordinator` or `storage/postgres` are
  needed for the contract table itself.

- **Add a new RunStatus** → add the constant to `types.go`; add the value to
  `RunStatus.Validate`; add the transition condition to `ReconcileRunProgress`
  in `progress.go`; update `progress_test.go` with positive and negative cases.
  Notify the storage layer — the Postgres WorkflowControlStore may need
  a matching DB value if it stores the status string directly.

- **Add a new claim operation** → add the method to `ControlStore` in
  `store.go`; implement in the Postgres WorkflowControlStore;
  add test coverage in `storage/postgres` tests. No changes to value types
  are usually needed unless the operation introduces new fields.

- **Change a default timing value** → edit `defaults.go`; search for all
  callers of the changed default accessor (primarily coordinator's `config.go`);
  verify that the coordinator's heartbeat-interval-less-than-lease-TTL
  invariant still holds with the new defaults.

- **Add a field to WorkItem or Claim** → add the field in `types.go`; update
  `Validate` if the field has identity or lifecycle constraints; update
  `storage/postgres` scan and write paths; add test coverage in `types_test.go`.

## Failure modes and how to debug

- Symptom: `ReconcileRunProgress` returns `RunStatusComplete` unexpectedly →
  check that `CollectorRunProgress.PublishedPhaseCounts` is populated with all
  keys from `RequiredPhasesForCollector`; a missing key counts as zero and may
  satisfy the "all phases ready" check if `TotalWorkItems == 0`.

- Symptom: new collector kind produces no completeness rows →
  check that the collector kind is registered in `collectorContracts`; an
  unknown kind returns an empty `RequiredPhasesForCollector` slice.

- Symptom: `FamilyFairnessScheduler.Next` returns the same target repeatedly →
  only one family or instance is present; this is correct weighted behavior,
  not a bug. Add more `FairnessCandidate` entries to distribute load.

- Symptom: `DesiredCollectorInstance.Validate` returns an error for a valid
  config → check that `Mode` is one of `continuous`, `scheduled`, or `manual`,
  and that `Configuration` is valid JSON (empty string is normalized to `{}`
  by `normalizeJSONDocument`).

- Symptom: `Claim.Validate` fails with `lease_expires_at must not be before
  heartbeat_at` → the storage layer is writing a `LeaseExpiresAt` that is
  earlier than `HeartbeatAt`; check the lease duration calculation in
  `storage/postgres`.

## Anti-patterns specific to this package

- **Adding database calls to this package** — `internal/workflow` is a pure
  contract package. All Postgres interaction belongs in `internal/storage/postgres`
  behind `ControlStore`.

- **Branching on collector kind outside collectorContracts** — do not add
  `if kind == scope.CollectorGit` conditionals in `progress.go` or `fairness.go`.
  Per-family behavior belongs in the `collectorContracts` table.

- **Sharing one FamilyFairnessScheduler across goroutines without a mutex** —
  `Next` mutates state; this will cause data races under the Go race detector.

- **Silently accepting unknown RunStatus values** — `RunStatus.Validate`
  rejects unknown values by design. Do not add a default catch-all case that
  returns nil; unknown statuses are a storage/migration problem that must be
  surfaced.

- **Treating zero CollectorRunProgress.TotalWorkItems as error** —
  `ReconcileRunProgress` handles zero totals gracefully (returns
  `collection_pending`). Do not add a non-nil guard at call sites.

## What NOT to change without a design discussion

- `ControlStore` interface — it is the shared contract between coordinator,
  ingester collectors, and the storage layer; changes require coordinated
  updates across all three.
- `collectorContracts` entries once they have storage-side phase publication
  records — removing a required phase means existing completeness rows become
  stale; coordinate with the reducer team.
- Fencing token semantics in `WorkItem` and `Claim` — the `FencingToken`
  field provides optimistic concurrency for claim mutations in Postgres;
  changing its meaning breaks the storage-layer CAS pattern.
