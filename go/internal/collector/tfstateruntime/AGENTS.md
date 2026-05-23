# tfstateruntime Agent Guidance

## Read First

1. `README.md` and `doc.go` for runtime scope.
2. `source.go` and `source_helpers.go` for claim matching, source opening, and
   generation construction.
3. `fact_spool.go` and `composite_capture_recorder.go` before changing parser
   output handling.
4. `go/internal/collector/terraformstate/README.md` for reader/parser safety.
5. `go/internal/collector/claimed_service.go` for claim lifecycle,
   heartbeats, and fencing behavior.

## Local Rules

- Never persist, log, or put raw Terraform state bytes in errors, facts, spans,
  metrics, status, docs, or PR text.
- Open only exact candidates from `terraformstate.DiscoveryResolver`. Do not
  guess repo-local `.tfstate` files, S3 prefixes, workspace directories, or
  unapproved source locators.
- Keep AWS SDK types out of this package; use reader interfaces such as
  `terraformstate.S3ObjectClient`.
- Return a generation only when claim scope ID, generation ID, and source run
  ID match the state snapshot identity.
- Copy the current workflow fencing token into every emitted fact.
- Treat missing or oversized state as warning-only generations; keep transient
  source-open failures retryable.
- Keep raw locators, bucket names, local paths, work item IDs, and secrets out
  of metric labels. Use bounded hashes or warning kinds.

## Change Rules

- Add backends only after the reader package exposes a safe exact-source type.
- Wire optional parser recorders through `ClaimedSource` so counters and logs
  remain testable.
- Keep cloud SDK adapters in command/integration wiring, not parser code.
- Do not add storage, graph, reducer, query, or workflow-planning ownership to
  this runtime adapter.
