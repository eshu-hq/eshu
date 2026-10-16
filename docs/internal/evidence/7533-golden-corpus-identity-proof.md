# B-7 PostgreSQL identity runner enrollment

Refs #7533. This batch enrolls two existing PostgreSQL proofs in the
`golden-corpus-identity` dedicated runner. Both Go sources remain unchanged.
The runner uses the existing B-7 job on pull requests to main, pushes to main,
and selected merge groups. It adds no schedule or backend support claim.

## Contract

| File under `go/` | Exact proof in its declared package |
| --- | --- |
| `internal/reducer/containerimage/container_image_identity_support_writer_demotion_postgres_live_test.go` | `TestContainerImageIdentitySupportWriterRetiresPromotedDecisionOnDemotionLive` |
| `internal/storage/postgres/container_image_identity_cloud_reopen_ordering_postgres_live_test.go` | `TestContainerImageIdentityCloudReferenceReopenOrderingPostgresLive` |

The closed mapping in `scripts/lib/golden_corpus_identity.py` requires both rows
with `class: dedicated_ci`, `tag: ~`, and `runner: golden-corpus-identity`.
Neither row can assert graph backends. Missing, extra, duplicate, or demoted
rows fail validation, including a demotion with a newly valid legacy digest.
The new base includes the seven Ifá rows merged by #8245 and has 523
legacy exemptions. This patch enrolls two more rows, reducing the count to
521. The digest binds the full remaining inventory.

The actual shell guard requires one selected test run, one selected pass, and
one successful terminal event from the expected package, in that order.
Selected skips, failures, duplicate terminals, malformed events, and the same
test name in a different package fail. Unrelated no-test packages remain valid
because the demotion helper selects `./internal/reducer/...`.

The existing commands, selectors, DSNs, workloads, and 60s/120s Go test timeouts
remain unchanged. A failed Go process still fails the helper even if stdout
contains successful events. Stdout JSON and stderr remain separate.

## Local proof, 2026-10-16

Before the change, the actual shell guard accepted a matching test name from
an incorrect package without any package terminal event. The regression exited
1 against that implementation. The new guard rejects that stream.

The committed hermetic suite exercises both actual shell helpers and the real
ledger validator. It also mutates source test sets, driver calls, workflow
activation, owner steps, and individual B-7 registry routes. The existing B-7
mirror invokes the suite and validates the ledger.

A fresh owned PostgreSQL 18.6 database used the pinned CI PostgreSQL image:
`ghcr.io/eshu-hq/ci-postgres-alpine@sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873`.
An ephemeral Go caller invoked `postgres.ApplyBootstrap` through `postgres.SQLDB`;
it copied no migration SQL and performed no graph bootstrap. Its SHA-256 was
`de3a0de593baff6e9f42a50b4398fc176a2bfd6407d07db6f16d08b1d149c7fd`.
Bootstrap exited 0 in 12.14 seconds. The caller was removed before static gates.

Both updated shell helpers executed their real Go commands and exited 0:

| Helper | Wall seconds, including Go compilation | Selected run/pass | Package terminal |
| --- | ---: | --- | --- |
| Demotion | 42.85 | 1 / 1 | pass |
| Cloud reopen ordering | 84.85 | 1 / 1 | pass |

These are scoped helper measurements, with no speedup or full B-7 claim.
Raw stdout and stderr, direct process exits, source hashes, and backend identity
were retained with the local proof. The unchanged Go source SHA-256 values are:

- Demotion: `c3abb0b93c2fb93e9255b1d77b431e44de7916460b43937bb7fbc0235bb643d2`.
- Cloud reopen: `664ca83d8d875427ab164d369b4e10e8495290fc06b1e53a24e6da1601882522`.

## Acceptance boundary

The local helper results do not replace the canonical hosted B-7 gate.
The final hosted B-7 run must execute both updated guards successfully before
merge. Both hosted B-7 cells passed on the prior published head `cd603442`.
This rebase changes the ledger and validator; hosted B-7 acceptance at
the new head is NOT_CHECKED.
No production Go, SQL, golden snapshot, or backend standard changes here.
