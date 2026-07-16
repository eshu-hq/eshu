<!-- docs-catalog
title: Run The Proof Suite
description: Runs make pre-pr and make prove, explains what each selects, and shows how to read the output.
type: how-to
audience: practitioner
entrypoint: true
landing: false
-->

# Run the proof suite

Two commands cover almost every Ifá change, but they run at different stages:
`make prove` is focused conformance proof; `make pre-pr` is the late general
promotion gate. Use the order below before opening or updating a PR that touches
`go/internal/ifa`, `go/cmd/ifa`, or anything the gate registry maps to them.

## 1. Run focused proof with `make prove`

```bash
make prove
```

`make prove` always runs a credential-free common path, then adds the Docker
matrix when your changed paths select it and Docker is present:

- **Contract-layer tests** — `go test ./internal/ifa/... ./cmd/ifa -count=1`.
- **Hermetic determinism structural mirror** —
  `bash scripts/test-verify-ifa-determinism.sh`, a fast in-process proxy for
  the real Docker-backed determinism matrix.
- **Hermetic dead-letter-matrix structural mirror** —
  `bash scripts/test-verify-ifa-dead-letter-matrix.sh`, the same proxy for the
  failure-path determinism matrix.
- **Coverage reconcile** — `ifa coverage`, in its default advisory mode. An
  uncovered surface is a backfill item, not a failure; the blocking coverage
  check lives in `ifa-contract-layer`'s own test.
- **The Docker matrix**, only when the changed-path selector picks
  `ifa-determinism` or `ifa-dead-letter-matrix` and Docker is running.
  Without Docker, `make prove` prints operator guidance and defers loudly —
  it never reports a silent pass.

## 2. Review, promote once, and review the exact diff

After focused proof is green:

1. Run a preliminary full `eshu-code-review` of the rebased diff. Fix every P0,
   P1, and P2 finding, rerun affected focused proof, and repeat the full review
   until its verdict is `P0=0, P1=0, P2=0`. Do not run `make pre-pr` sooner.
2. When the branch is otherwise ready to push, run `make pre-pr` exactly once
   as the late promotion gate.
3. Run a final full `eshu-code-review` against the exact post-preflight diff.
   Make no edits before push; any diff change invalidates the verdict and
   restarts the sequence at focused proof.

```bash
make pre-pr
```

This runs gofumpt and golangci-lint over the whole module, `go build` and
`go vet`, the focused tests for packages you changed, the 500-line file cap
and package-docs gates, and the credential-free exactness gates your changed
paths select from `specs/ci-gates.v1.yaml` — including `ifa-contract-layer`
and `ifa-load-saturation` when you touch `go/internal/ifa` or `go/cmd/ifa`.

To see exactly which gates your branch selects, and why:

```bash
bash scripts/dev/select-gates.sh --base origin/main --tier pre-pr --explain
```

## Reading the output

`make prove` ends with a deterministic report and a separate timing block:

```text
==== PROVE REPORT (deterministic) ====
ifa contract-layer tests                       PASS
hermetic determinism structural mirror         PASS
hermetic dead-letter-matrix structural mirror  PASS
ifa coverage reconcile (advisory)              PASS
docker matrix: graph-determinism (Layer 2)     SKIP (not selected for changed paths)
docker matrix: dead-letter-set determinism (Layer 2) SKIP (not selected for changed paths)
==== END PROVE REPORT ====
```

The report block is fixed-order and token-free by design: two runs against
the same repo state produce byte-identical lines here, so you can diff two
runs to spot a real behavior change. Wall-clock numbers live only in the
timing block that follows, never in the report itself.

A coverage run also prints per-surface findings before the report:

```text
  [PASS] narrowed_correlation:rc-29: covered: odù "odu:kustomize-deploys-from": relationship DEPLOYS_FROM carries required evidence kind(s) [KUSTOMIZE_RESOURCE_REFERENCE] across 1 matching evidence fact(s) (>= 1)
  [WARN] narrowed_correlation:rc-30: uncovered: no replay scenario mapped for required scenario_type baseline
```

`WARN` lines are the honest backlog of surfaces nobody has bound an Odù to
yet — they do not fail the advisory run. A surface only turns `FAIL` when
`ifa coverage -blocking` runs (the mode CI uses) and the surface is stale or
resolves to the wrong evidence.

## The five Ifá CI gates

| Gate id | What it proves |
| --- | --- |
| `ifa-contract-layer` | Payload schema, typed round-trip, and derived graph-evidence coverage for every cataloged Odù. |
| `ifa-determinism` | The canonical graph is byte-identical across worker counts 1, 2, and 4. |
| `ifa-dead-letter-matrix` | A schema-major-mutated Odù dead-letters the identical durable set across worker counts. |
| `ifa-load-saturation` | The corpus amplifier and the backpressure/saturation regression proof (issue #3560). |
| `ifa-fault-injection` | Lease reclaim, retry, and idempotent replay converge under five injected faults. |

See [CI gates reference](../reference/ci-gates.md) for the full, generated
table of every gate in the registry, and
[Debug a failing gate](debug-a-failing-gate.md) when one of these five turns
red.
