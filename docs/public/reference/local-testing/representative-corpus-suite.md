# Representative Corpus Suite

Issue #3169 tracks reducer, graph-write, and collector-growth scale work. The
first gate is the representative corpus contract in
`specs/scale-lab-corpus.v1.yaml`; issue #3170 must be accepted before the
benchmark, query-plan, or correlation-fanout implementation issues begin.

## What The Contract Covers

The suite defines five proof slots:

| Slot | Purpose |
| --- | --- |
| `smoke/synthetic_contracts` | Package-local behavior, edge cases, and validator tests. |
| `small/single_repo_multidomain` | One sanitized multi-domain repository proof. |
| `medium/representative_20_50` | Default 20-50 repository representative scale gate. |
| `large/full_corpus_release` | Release or high-risk runtime confidence proof. |
| `pathological/fanout_correlation` | High-cardinality correlation fixture that must bound or refuse unsafe fanout. |

Every accepted proof must cover code relationships, supply-chain evidence,
cloud/IaC/runtime correlation, docs, incidents, and observability. Missing
evidence is allowed only when it is explicit, truth-labeled, and consistent
across API, MCP, CLI, graph truth, and read-model truth.

## Required Metrics

The contract requires the following metrics before runtime-affecting scale work
can claim readiness. Issue #3171 records the result artifact shape in
[Scale Benchmark Artifact](scale-benchmark-artifact.md):

- fact rows per second;
- queue claim latency p95;
- reducer drain wall time;
- graph write p95;
- API and MCP p95;
- retry and dead-letter counts;
- memory high-water mark;
- correlation fanout candidate p95;
- graph query-plan regression count.

Runtime results must stay within the same-shape accepted baseline by no more
than 10 percent or 60 seconds, whichever is larger, unless the PR carries an
owner-approved exception and a follow-up issue.

## Privacy Contract

Public docs, issues, PRs, and evidence must contain only aggregate counts,
fixture ids, public issue numbers, status enums, version ids, and sanitized run
ids. Private source locators, provider payloads, hostnames, IP addresses,
account ids, package names from private systems, local paths, raw alerts, and
raw transcripts stay outside the repository.

## Verification

Run:

```bash
bash scripts/verify-scale-corpus-suite.sh
bash scripts/test-verify-scale-corpus-suite.sh
bash scripts/verify-scale-benchmark-artifact.sh
bash scripts/test-verify-scale-benchmark-artifact.sh
```

The verifier checks that the published contract keeps all required slots,
domains, privacy rules, metrics, thresholds, and downstream gate links. It also
fails if the spec contains private-looking values.
