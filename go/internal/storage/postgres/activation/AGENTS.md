# AGENTS.md — storage/postgres/activation guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md`, especially the Ack atomicity and lock-order invariants.
3. `sql.go` (every statement), `finalize.go`, `backlog.go`, `port.go`.
4. `../migrations/160_activation_obligations.sql`.

## Invariants

- `Insert` is one idempotent row write run inside `ProjectorQueue.Ack`'s
  transaction, after the generation is activated and before commit. Never
  move it after commit, never add a read, graph call or network call to it.
- `Finalize` locks the scope row, then the obligation row. Every blocking
  path in the parent package takes the scope row first; keep that order or
  Finalize can deadlock (40P01) with Ack, Fail or the ingestion commit.
- `Finalize` never publishes a phase and never runs maintenance. The phase
  check pins every key column to the obligation's own generation; another
  generation's phase must never satisfy it.
- The wake touches only `deployment_mapping` rows of the exact scope and
  generation that are `retrying` with `cross_repo_backward_evidence_not_ready`
  and hold no lease, uses `SKIP LOCKED`, and changes only `visible_at`,
  `next_attempt_at` and `updated_at`. Keep the row-self predicates repeated in
  the UPDATE for the EvalPlanQual recheck, and keep the cap at
  `WakeBatchLimit`.
- `obsolete` is only for a moved or NULL active pointer; `inapplicable` is
  only for a generation that can never carry a phase (no repository fact, or
  the maintainer's `ErrActivationInapplicable`). Prune MUST NOT delete
  `inapplicable` rows: CatchUp skips any generation with a row, so pruning
  one would let CatchUp owe it again. Never add an attempt cap that marks a
  row terminal: it would drop an owed phase silently (#7584 ruling D2).
- Every completion, obsolete and inapplicable write is fenced on `state = 'leased'`,
  `claim_token`, `lease_owner` and `lease_until > clock_timestamp()`. Use the
  database clock for every lease comparison.
- A partial wake commits only after `stillOwnedQuery` confirms the lease.
- `CatchUp` reads a bounded page of `ingestion_scopes` by scope count, not by
  matches, and only owes repository generations (a repository fact exists).
- Never import the parent `postgres` package from here.

## Verification

```bash
cd go && go test ./internal/storage/postgres/activation -count=1
ESHU_DEFERRED_PARTITION_PROOF_DSN=<admin dsn> ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1 \
  go test ./internal/storage/postgres -run '^TestActivationObligation' -count=1
```
