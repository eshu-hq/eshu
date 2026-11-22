# Source Pacing

The coordinator section of the pipeline, collector, ingester, and
hosted-readiness payloads carries a `source_pacing` list with one row
per source system under per-source reducer backpressure (#10882).

| Field | Present | Meaning |
| --- | --- | --- |
| `source_system` | Always | The slowed or observed source system (for example `git` or `vuln-intel`). |
| `slowed` | Always | Whether intake from the source is currently slowed. This is the shared latch truth: the latch engages when at least `ESHU_SOURCE_BACKPRESSURE_DEPTH_FLOOR` claimable rows are older than the engage age and releases when fewer than that many are older than the release age. |
| `reason` | When slowed | Why intake slowed (`source_behind`). |
| `oldest_claimable_age`, `oldest_claimable_age_seconds` | Always | Age of the source's oldest claimable outstanding reducer row at read time. |
| `claimable_depth` | Always | Count of the source's claimable outstanding reducer rows at read time (all ages). The slowed verdict counts only rows past the engage and release ages, so this total can sit far above the floor on a source that is not slowed. |
| `engaged_since` | When slowed | When the current slowed engagement began. |
| `stuck` | When stuck | Whether the engagement outlasted `ESHU_SOURCE_BACKPRESSURE_MAX_ENGAGEMENT` with the queue still behind, meaning at least `ESHU_SOURCE_BACKPRESSURE_DEPTH_FLOOR` claimable rows are still older than the release age (the same rule the consult uses to release): still slowed, and loud. An engagement that has outlasted the bound but has fewer rows left than the floor is not stuck; the next consult releases it. It is evaluated only by a process that has the `ESHU_SOURCE_BACKPRESSURE_*` thresholds: the API process on a live read, or the reducer's status summary writer, which stores the verdict with the row (so an API pod without the env still serves it from the stored summary). A process without the thresholds reports `stuck` absent. |

Rows carry only source identity and bounded queue numbers, so scoped
callers are withheld the whole section like the other process-global
aggregates. The row shape is pinned by the `SourcePacing` component in
the OpenAPI document.
