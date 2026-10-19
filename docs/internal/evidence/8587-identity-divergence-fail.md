# Evidence: semantic identity-divergence fails non-retryably (#8587)

## Change

`go/internal/reducer/code/semantic/materialization.go`: after the
`logSemanticEntityUnmatchedUIDs` WARN, `Handle` returns
`semanticEntityIdentityDivergentError(writeResult)` (new constructor in
`materialization_helpers.go`) instead of succeeding. A plain `fmt.Errorf`
is deliberate: `reducercontract.IsRetryable` defaults false for errors
that do not opt in, so the queue dead-letters the item once
(projection_bug/manual-review bucket) instead of retry-looping a
poison pill. No Cypher change, no queue/lease change, no new retry
machinery; race-plausible rows (verify counts them matched) still
succeed WARN-free.

## Ordering proof (criterion 1)

Within-generation ordering is total by construction:
`go/internal/projector/runtime/projection.go` runs `writeCanonicalProjection`
(line 206, synchronously MERGEs canonical nodes via `CanonicalNodeWriter`)
before `IntentWriter.Enqueue` (line 218). A derivation-agreeing row's node
therefore exists before its semantic work item can run. Residual windows
are stale-side only: no freshness gate on the semantic handler means a
superseded generation can still write (last-writer-wins on cleared
properties), and a canonical-reap delete can beat a stale item's MATCH.
Neither can afflict the freshest generation's derivation-agreeing rows,
so every unmatched uid is a deterministic identity divergence — safe to
fail without an ordering proof for the race class.

## No-Regression Evidence: matched path is cost- and behavior-identical

- The `len(UnmatchedEntityIDs) > 0` branch already ran for the WARN; the
  matched path executes zero new instructions.
- The error constructor runs only on the previously-succeeding unmatched
  path: one bounded `fmt.Errorf` (uid list capped at
  `maxSemanticUnmatchedUIDsLogged = 10`, same cap as the WARN).
- `go test -count=1 ./internal/reducer/code/semantic/` green, including
  the seeded RED/GREEN
  `TestSemanticEntityMaterializationHandlerFailsOnIdentityDivergentUIDs`
  (failed pre-fix with `Handle() error = nil`, passes post-fix) and the
  updated `TestSemanticEntityMaterializationHandlerWarnsOnUnmatchedUIDs`
  (WARN assertions kept, now expects the failure per the superseded
  contract).
- `go test -count=1 -run 'TestVerifySemanticEntityMatchIdentities|
  TestSemanticEntity' ./internal/storage/cypher/` green: the verify
  function is untouched.
- `TestSemanticEntityMaterializationHandlerSilentWhenAllRowsMatched`
  green unchanged: fully-matched writes still succeed with no WARN and no
  error (race-plausible class stays WARN-only by construction).

## Observability Evidence: WARN kept, failure is the new terminal signal

- The `semantic entity rows matched no canonical node` WARN is unchanged
  (same attrs, same caps); the completion record still carries
  attempted-vs-matched counts.
- New: the work item fails non-retryably with the bounded uid list and
  the landed-matched count in the message, so the dead-letter record
  tells the operator the matched rows already landed and only the named
  uids need a derivation fix. No new spans or metrics.
