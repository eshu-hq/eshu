# #6296 — the Helm NornicDB pin moves to v1.2.3 by digest

`deploy/helm/eshu/values.yaml` pinned `timothyswt/nornicdb-cpu-bge` at
`v1.1.11@sha256:51b6174a…`. The operator runs **v1.2.3**. This record covers what
moving that pin does and does not establish.

## The tag is not the version

`v1.2.3` self-reports `1.2.2` internally — a running container logs
`"version":"1.2.2"`. The chart therefore pins the **digest**
`sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74`, not the
tag. Anyone bumping this again should read the version out of a running
container rather than trusting the tag name.

## Why this digest

It is the same artifact `scripts/verify-replay-tier.sh:24` already runs the R-5
replay gate against, and the same one cited in
`go/internal/query/code_relationships_nornicdb_optional_match_live_test.go:16`.
Reusing it puts the chart and the replay gate on one build instead of two.

That matters for coverage: before this change the chart's image had **no gate
coverage at all**. The B-7 golden-corpus gate and the e2e workflows all drive
`docker-compose.yaml`, which is the PR#290 source build, not the chart's image.
After it, the deployed artifact is one the replay tier already exercises.

## What is measured, and on which build

`docs/internal/evidence/6176-grouped-semantic-retract-version-floor.md` records
the one behaviour measured across versions — the grouped semantic delta-retract,
20 runs per build:

| build | reports | grouped retract |
| --- | --- | --- |
| `eshu-nornicdb-pr290:3722b483c02c` | 1.2.1 | PASS 20/20 |
| `timothyswt/nornicdb-cpu-bge:v1.2.3` | 1.2.2 | PASS 20/20 |
| `sha256:51b6174a` (the old chart pin) | 1.1.11 | **FAIL 20/20** |

So the pin moves off the only build in that table where a measured behaviour is
broken.

No-Regression Evidence: no Eshu code path changed. The diff is the chart's image
reference, the design-doc claim a test asserts against, and operator-facing docs
that named the old version. No Cypher, query shape, writer, worker knob, batch
size, lease or concurrency setting is touched, so there is no Eshu-side path
whose cost could change. The backend binary itself changes, and **its throughput
was not measured** — see below.

Observability Evidence: no metric, span, log line or status field changes. The
one operator-visible difference is the image reference the chart renders, and the
version a running container self-reports (`1.2.2`), which is already logged at
startup by the backend itself.

## What this does NOT establish

- **No throughput or latency comparison between v1.1.11 and v1.2.3.** Nothing
  here says the new build is faster, slower, or equal under Eshu's load. The
  replay tier proves functional correctness on this digest, not performance.
- **The `relationshipMergePropertyIdentity` capability is unmeasured on this
  build.** `deploy/helm/eshu/values.yaml` still sets it `false` and
  `templates/validate.yaml:38` still fails the render closed without it. Nothing
  in this repository states whether published v1.2.3 preserves relationship MERGE
  identity properties (orneryd/NornicDB#290), and a version comparison is not a
  measurement. The flag was deliberately not flipped.
- **Eleven of the twelve v1.1.11 workaround classes remain unmeasured on 1.2.2.**
  Sixty-four comment sites under `go/internal/storage/cypher/` cite v1.1.11 as
  the reason for a workaround. Only the grouped semantic retract has a number.
  Moving the pin is not evidence that any of those workarounds can be removed;
  each needs its own measurement on the new build first.
