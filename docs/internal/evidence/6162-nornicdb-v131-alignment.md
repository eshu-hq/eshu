# NornicDB v1.3.1 alignment evidence (#6162)

## Decision

Eshu's current Compose, Helm, R-5 replay, and Kubernetes governance-proof
defaults use one immutable upstream artifact:

```text
timothyswt/nornicdb-cpu-bge:v1.3.1@sha256:ac52489925968e39d18f845bde5fa2fe363ba703443ead7f97ebc2b0c0084962
```

The OCI index contains the following Linux manifests:

| Platform | Manifest digest | Validation in this change |
| --- | --- | --- |
| `linux/amd64` | `sha256:c0b5f73c55bd56a6764d1833665252b98a30b332248f0233f5f4eab4dc0d2ca1` | Docker runtime and live Eshu proof |
| `linux/arm64` | `sha256:d787abe61d92c67761bdbd21ae5224aad904f2a13283d46d13fdfb6269b86b7b` | Manifest and verifier branch only; no arm64 runtime claim |

The amd64 container reported `NornicDB v1.3.1`. The published image has no
OCI source-revision label, so the immutable index digest, selected platform
manifest, platform, and binary-reported version form its provenance contract.

## Why the prior default was rejected

The prior Compose source build, `eshu-nornicdb-pr290:3722b483c02c`, passed a
fault-free baseline but corrupted the graph after a backend restart while the
queue still reached a terminal state. On the eight-project Ifá fixture, the
baseline had 678 nodes, 707 edges, and 627 GCP relationships with digest
`280a882458096e6813cb4f3d7c6552b92860c5b4c2a6e597ee5cc69c462f8052`.
The restart result had 678 nodes, 833 edges, and 753 GCP relationships with
digest
`51e00ff9afffd104d403a8f94cb7f5b31ca2d3c1b30362a79e8e3fa2436fe0fe`;
126 GCP relationships joined endpoints from different projects. This was a
false-green convergence signal, not missing retry or dead-letter handling.

Replacing only the backend artifact with the v1.3.1 digest removed the
cross-project relationships and restored the baseline digest. The permanent
Ifá assertion now checks the exact fixture scope-to-project map, expected edge
counts, `reducer/gcp-relationships` producer, `CloudResource` endpoint labels,
and both endpoint `account_id` values before accepting either baseline or
restart output.

## Local proof on 2026-09-11

All commands ran from the feature worktree with `GOTOOLCHAIN=go1.26.6` and
`CC=clang` for Go commands that compile tree-sitter.

| Contract | Command | Result |
| --- | --- | --- |
| Image and package defaults | `docker compose config --quiet` and `helm lint deploy/helm/eshu` | pass |
| Runtime and Ifá contracts | `cd go && go test ./internal/ifa/graphdump ./cmd/ifa ./internal/runtime ./internal/query/codequery/relationships -count=1` | pass |
| Ifá hostile/static mirror | `bash scripts/test-verify-ifa-fault-injection.sh` | pass; 49 cells and four-shard exact cover |
| Kubernetes provenance verifier | `bash scripts/test-verify-k8s-two-team-governance-proof.sh` | pass for the exact index or amd64 child on `linux/amd64`; arm64, wrong index, repository, platform child, version, and invented source revision fail closed |
| Governance selector controls | `bash scripts/test-verify-two-team-governance-proof.sh` and `bash scripts/test-verify-k8s-two-team-governance-proof.sh` | pass; own-selector missing, non-200, wrong-identity, and API/MCP divergence cases fail closed before cross-scope 403/404 evidence is accepted |
| Helm storage migration | isolated Minikube install of chart `0.0.3-pre-release-21` at `a281fad7523b`, upgrade to candidate `0.0.3-pre-release-23`, and rollback | pass; the failed unacknowledged upgrade mutated neither the Helm revision, Deployment contract, nor PVC contract; the acknowledged upgrade switched the exact v1.2.3 image and legacy claim to the exact v1.3.1 image and fresh claim; rollback restored the old image, claim, and sentinel |
| Remote-evidence gate selection | `bash scripts/test-verify-remote-validation-artifacts.sh` | 34 passed; runner, verifier, helper, test, and fixture paths select the gate |
| Relationship identity | `cd go && go test ./internal/storage/cypher -run 'TestProvenanceEdgeWriterLive(LegacyRowSetMigration|SamePairAssertionIsolation)' -count=1 -v` against the exact v1.3.1 amd64 container | pass; legacy migration, duplicate delivery, eight-way concurrent delivery, retry, and scoped retract isolation |
| R-5 replay | `bash scripts/verify-replay-tier.sh` | pass; offline graph truth and tombstone/idempotent replay completed in 87 seconds; SQL UNION branches passed live in 48 seconds |
| B-7 golden corpus | `bash scripts/verify-golden-corpus-gate.sh` | 561 pass, 0 required failures, 1 advisory timing warning; 147 seconds total against the 1,800-second blocking ceiling |

No-Regression Evidence: This alignment makes no cross-version speedup claim:
wall times from the former source-built backend and the published v1.3.1
artifact are not treated as comparable. On the final immutable NornicDB v1.3.1
index, R-5 completed offline graph-truth and tombstone/idempotent replay in 87
seconds and its live SQL UNION branches in 48 seconds. B-7 completed the
31-repository, 19-collector-source, 37-scope-generation corpus in 147 seconds
against the 1,800-second blocking ceiling with 561 passes, zero required
failures, and zero residual, dead-letter, required-shared-intent,
cross-scope-completion, or unroutable work; one maintenance drain took 33
seconds against the unchanged 30-second advisory target. The same-v1.3.1 Ifá
eight-project comparison used the same corpus, worker count, terminal
conditions, storage reset, and graph boundary: baseline completed in 18 seconds
and restart in 20 seconds, both produced 627 GCP relationships with
`cross_scope=0`, zero dead letters, terminal queue counts, and the identical
canonical graph digest. This proves the final artifact meets the repository's
blocking correctness and performance acceptance and preserves restart graph
truth; it does not prove a speedup over the historical backend or improved
maintenance-drain latency.

## Live local Kubernetes proof

The exact implementation commit `98589eba1` passed
`bash scripts/run-k8s-two-team-governance-proof.sh --artifacts <temporary-dir>`
on a disposable single-node `linux/amd64` Minikube v1.39.0 / Kubernetes v1.37.0
cluster using the Docker runtime and Calico, under a 20-minute outer bound. The
run included the post-rebase Eshu image build, Helm deployment, two-repo seed,
API and MCP capture, artifact verification, and namespace cleanup.

The Pod specification used the immutable v1.3.1 multi-architecture index. The
runtime reported that same immutable index, the node reported `linux/amd64`,
and the running binary reported `NornicDB v1.3.1`. The verifier passed:

- unauthenticated API and MCP rejection;
- admin visibility of both seeded repositories;
- one-repository visibility for each team through both API and MCP;
- each team's own repository selector returned `200` and the exact expected
  repository ID through both API and MCP;
- cross-scope omission and matching non-disclosing `404` selectors;
- API/MCP parity;
- four applied NetworkPolicy objects with the restricted-egress chart mode;
- exact NornicDB provenance and artifact redaction.

The normalized six-file artifact checksum was
`sha256:8b8e05399b5e74cb6e77b89b395d8cb070823e37ad804ac5157c985942f742fb`.
This is local single-node evidence, not a production, managed-cluster,
multi-node, arm64-runtime, or in-place-upgrade claim. It proves the restricted
NetworkPolicy objects were applied on a Calico-backed cluster; it is not a
hostile packet-flow test.

## Restart determinism

One full `--shard 4/4` Ifá run passed every assigned recovery cell. Its common
baseline completed in 24 seconds. The restart cell completed in 21 seconds,
fired the restart sentinel during the drain, reached zero dead letters and
terminal queue counts, checked 627 GCP relationships with `cross_scope=0`, and
matched the baseline digest:

```text
280a882458096e6813cb4f3d7c6552b92860c5b4c2a6e597ee5cc69c462f8052
```

The supported sharder was then checked before a second run:

```text
$ bash scripts/verify-ifa-fault-injection.sh --list-cells --shard 4/21
cell_baseline
cell_restartbackend
```

That independent focused run again passed. After the final rebase, the same
slice was repeated: its baseline completed in 18 seconds and restart completed
in 20 seconds, fired the sentinel, reached zero
dead letters, checked the same 627 GCP relationships with `cross_scope=0`, and
produced the same digest in both cells. The same corpus, worker count, terminal
conditions, storage reset, and graph boundary were used in both runs.

## Storage and rollback boundary

This change does not claim that an existing graph volume can be upgraded in
place or downgraded safely. Compose selects a new `nornicdb_v131_data` volume.
Helm selects a retained `<release>-nornicdb-v131-data` claim and keeps an
existing legacy claim without mounting it. A live upgrade with a legacy claim
fails until the operator acknowledges fresh-volume migration after stopping
writers and preserving the old graph. The keeper uses the prior chart-authored
PVC shape rather than API-defaulted or binder-owned fields.

Two isolated Minikube upgrade/rollback runs covered defaulted and explicitly
configured storage classes. The defaulted-class run began at the exact prior
chart `0.0.3-pre-release-21`, whose running image was
`v1.2.3@sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74`
on the legacy PVC. It wrote and read back a sentinel, then proved an
unacknowledged upgrade changed neither Helm revision, Deployment contract, nor
PVC contract. The acknowledged candidate ran the exact v1.3.1 digest on only
the fresh claim; the legacy claim kept the same UID and sentinel, while the
fresh claim did not contain that sentinel. Rollback restored the exact prior
image and legacy claim with the sentinel intact. Both NornicDB claims remained
retained, and the v1.3.1 claim was unmounted after rollback. The
explicit-class run rejected mismatched requested size and storage class before
rollout, accepted matching values, and rolled back successfully. This proves
storage isolation and rollback mechanics, not in-place format compatibility.
The supported operational migration still rebuilds from the durable Postgres
fact store and verifies queues and graph truth before cutover. An older
NornicDB binary must not open a volume modified by v1.3.1 without separate
reverse-compatibility proof.

## Operational signal

Root-Cause Evidence: PR #6657's NornicDB end-to-end job and the same focused
test against the pinned local v1.3.1 image both failed because the backend now
reports a relationship snapshot race with the delimiter `conflict detected:`.
The retry classifier recognized only the older `conflict:` delimiter, so the
retry remained safe but its bounded reason was the generic `transient_error`
instead of `write_conflict`. The exact live failure was
`Neo.TransientError.Transaction.Outdated` with `changed after transaction
start`; the sibling unique-conflict and stale-attribute regressions passed.
The classifier now maps the new spelling to `write_conflict` only when a typed
`Neo.TransientError.Transaction.Outdated` carries the ordered edge-or-node
identity and exact transaction-age suffix. Untyped or wrong-code lookalikes do
not gain retry authority; typed Outdated near-misses remain under the driver's
existing generic transient contract. The legacy raw `conflict:` fallback stays
available for older wrapped errors. After the change,
`./scripts/verify_backend_conformance_live.sh` passed all NornicDB retry
contracts and the related live backend regressions against v1.3.1.

That accepted run explicitly omitted the opt-in value-flow cloud-sink pair, as
the script reports. A separate opt-in run returned zero rows for that pair on
v1.3.1; this change therefore makes no value-flow cloud-sink support claim.

Observability Evidence: This alignment adds no Eshu runtime metric instrument,
label key, reason value, span, structured-log field, status schema, alert,
dashboard, worker or queue stage, or API/MCP response field. The exact v1.3.1
snapshot-conflict shape now uses the existing `write_conflict` reason instead
of the existing `transient_error` reason in
`eshu_dp_neo4j_deadlock_retries_total`; retry count, budget, backoff, span and
log fields, and reason cardinality are unchanged. Operators retain the existing
queue residual and dead-letter metrics and logs, graph-truth gates, container
health checks, and NornicDB binary-version output. The Ifá GCP scope assertion
and Kubernetes provenance JSON remain bounded CI/operator proof artifacts, not
deployed telemetry.
