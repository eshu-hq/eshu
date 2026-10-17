# storage/postgres/retention/statements

SQL text for the generation retention store in `storage/postgres`. The
package has no logic and imports nothing. The parent package binds and runs
each statement.

## Contents

| Constant | Used by | What it does |
| --- | --- | --- |
| `TargetedCandidateLock` | `generation_retention_relock.go` (as `generationRetentionTargetedCandidateQuery`) | Re-locks the provisionally selected candidates `FOR UPDATE OF generation, scope SKIP LOCKED`, in `(scope_id, generation_id)` order, and re-checks uncovered-writer coverage (#7903), delta-base protection (#7802) and the same-SHA exemption (#7981) at lock time. |

## Why it is a separate package

The parent directory is over the dirgate cap
(`go/cmd/dirgate`), so a new file there needs a growth trailer, and its
retention files sit at the 500-line cap. The move kept the statement text
byte for byte (#7427 restack onto #8139).

## Changing a statement

The statement must stay in lockstep with the parent's general candidate query
(`generationRetentionCandidateQuery` in `generation_retention_sql.go`). The
parent's tests read the text through the forwarding constant: the hard
ceiling, same-SHA and delta-base shape tests, and the re-lock live tests that
derive seeded variants from it. Run them after any edit.
