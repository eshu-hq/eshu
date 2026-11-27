# Digest-fenced deployment-evidence replay

Refs #11619. Date: 2026-11-27.

## Summary

The deployment-evidence replay reopened a finished workload item on every
rerun holding resolved edges. The fence hashes the resolved set once per
pass and reopens the item only when the hash changes. A rerun over an
unchanged set now skips the row entirely, so steady-state reruns do strictly
less work than before, with no new lock, round trip, or index.

## Environment

- Target: local compose PostgreSQL 18.6, disposable isolated schema per test.
- Build: branch `drive/11619-deployment-evidence-digest-fence`.
- Method: live queue tests drive the real UPDATE statements, then read the
  row back; the neighboring replay/retouch/enqueue/fence suite replays the
  legacy paths unchanged.

## Results

| Scenario | Before | After | Outcome |
| --- | --- | --- | --- |
| Rerun, resolved set unchanged | finished item reopened to pending | item stays finished, timestamps untouched | pass |
| Rerun, resolved set changed | item reopened | item reopened with the new digest recorded | pass |
| 8 concurrent reruns, set unchanged | 8 reopens | 8 quiet, row untouched | pass |
| Legacy callers (no digest either side) | reopened | reopened, same statement as before | pass |

No-Regression Evidence: the fenced statement keeps the unfenced shape (one
single-row primary-key UPDATE; the extra predicate is evaluated on the
already-locked row, so the plan cannot change and no index applies). The
digest is one sha256 over the already-held in-memory resolved identities.
`go test ./internal/storage/postgres/ -run 'Test11619|Replay|Retouch|
ReplayFence|FencedReplay|WorkloadReplay' -count=1` passes on live
PostgreSQL, as does the full `./internal/reducer/...` tree and `go vet` on
the touched packages. Observability Evidence: the `deployment mapping
materialization completed` log now carries `cross_repo_digest`, and
`workload_replay_count` drops to zero on quiet reruns; the recorded digest is
readable per item with `payload->>'deployment_evidence_digest'`.
