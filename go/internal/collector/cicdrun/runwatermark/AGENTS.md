# AGENTS.md - internal/collector/cicdrun/runwatermark guidance

## Read First

1. `README.md` - watermark contract and why it exists (#5429).
2. `types.go` - key, watermark, store interface, fencing validation.
3. `../ghactionsruntime/run_watermark.go` - gap-detection logic and wiring.
4. `../../awscloud/checkpoint/types.go` - the pattern this contract mirrors
   (fencing semantics, `Store` interface shape).
5. `../../../storage/postgres/cicd/watermark.go` - durable storage
   implementation.

## Invariants

- One `Key` (`scope_id`, `repository`) maps to exactly one watermark row.
  Do not add a sub-resource dimension unless a real second CI/CD provider
  or per-workflow watermark need appears; keep the contract as narrow as
  the actual caller.
- Inside one generation, `Save` MUST reject a fencing token strictly older
  than the stored row's (`ErrStaleFence`) and MUST accept a fencing token
  equal to the stored row's (idempotent redelivery). Do not change the
  comparison to strict `<` on both sides without checking every caller's
  retry assumption.
- Across generations, `Save` MUST NOT compare fencing tokens: a token counts
  claims of one work item, and each generation is its own work item, so a
  retried claim would refuse every later first attempt and wedge the watermark
  (#10063). It orders by numeric `LastRunID` instead, and `Validate` rejects a
  non-decimal one. `InMemoryStore` and the Postgres conflict filter state the
  same guard; change them together.
- This package has zero dependencies beyond the standard library. Do not
  import `ghactionsruntime`, `workflow`, or any provider client here --
  gap-detection logic and telemetry belong in the caller, not this
  contract package.
- `InMemoryStore` is not a production persistence answer by itself: it has
  no durability across process restarts or visibility across collector
  replicas. Do not present it as closing the #5429 gap without the
  Postgres-backed `Store` wired in `cmd/collector-cicd-run`.

## Common Changes

- Add a durable `Store` implementation in `storage/postgres` without
  importing `ghactionsruntime`.
- Update this package's README when the fencing semantics or the
  Key/Watermark shape changes.

## What Not To Change Without An ADR

- Do not make watermark state part of CI/CD fact payloads.
- Do not use `LastRunID`, `Repository`, or `ScopeID` as metric labels
  (high-cardinality/PII-adjacent) -- follow the fact-emission metric label
  discipline already documented in `../ghactionsruntime/README.md`.
- Do not turn gap detection into silent backfill-and-forget without an
  explicit `ci.warning` fact and metric; the whole point of #5429 is that
  loss must be visible, not quietly repaired.
