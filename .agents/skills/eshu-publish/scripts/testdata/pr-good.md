Refs #6950. Batch 3 of the compat-surface retirement.

**The `facts.*` docs compatibility layer is gone. All 110 caller files now import `facts/docs` directly, and the emptied `compat_docs.go` is deleted. No logic changes.**

| At a glance | |
|---|---|
| Behavior change | None. The 48 retired spellings were 28 const and type aliases plus 20 one-line forwarders. Callers now get identical values. |
| Size | 110 caller files edited, 1 file deleted |
| Proof | 20 of 21 test targets pass before and after, with the same set. The 1 failure also fails on the clean base on this host and passes in CI. |
| Review | Independent review: READY, after one P2 fixed in round 1 |

## What changed

- **Callers.** Every `facts.*` docs spelling became `docs.*`. 23 of them drop the `Documentation` prefix. Nine test files that already import another docs package use the `factsdocs` alias.
- **Detectors.** `kind_real_consumer.go` and `kind_real_consumer_dispatch.go` learn the `docs.*` spellings through a `factsPackageSelectorNames` table. The consumer and disclosure gate keeps passing, and the const lookup still fails closed.
- **Docs.** The `doc.go`, `README.md`, and `AGENTS.md` of `facts` and `facts/docs` now describe the retired surface in the past tense. The query and reducer READMEs use `docs.*`.
- **Ledger.** Rows `6950-docs-batch3-before` and `6950-docs-batch3-after` in `docs/internal/measurements.jsonl`.

## Proof

Before `c3cc407cb0`, after `22b2fa4a17`, 21 test targets.

| Check | Before | After |
|---|---|---|
| `go test -count=1` | 20 ok, 1 fail | 20 ok, 1 fail (same ok set) |
| `go build ./...` and `go vet ./...` | n/a | clean |
| Remaining `facts.*` docs references | 48 spellings in use | 0 (two intentional past-tense mentions in docs) |

<details>
<summary>Callers by package, and the gates that ran</summary>

- Callers: collector 49, query 16, doctruth 11, reducer 9, storage 6, semanticdocs 4, cmd 4, facts root 4, mcp 3, ifa 3, cli 1.
- The failing test is `TestFetchChurnZombiesDrainedByReaper`. It fails the same way on the clean base on this host.
- Gates green: `verify-fact-kind-registry`, `verify-factschema-diff`, `verify-payload-usage-manifest`, `verify-contracttest`, the MCP consumer-disclosure gate, and the compat-surface gate.

</details>
