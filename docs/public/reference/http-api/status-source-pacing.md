# Source Pacing

The coordinator section of the pipeline, collector, ingester, and
hosted-readiness payloads carries a `source_pacing` list with one row
per source system under per-source reducer backpressure (#10882).

| Field | Present | Meaning |
| --- | --- | --- |
| `source_system` | Always | The slowed or observed source system (for example `git` or `vuln-intel`). |
| `slowed` | Always | Whether intake from the source is currently slowed. |
| `reason` | When slowed | Why intake slowed (`source_behind`). |
| `oldest_claimable_age`, `oldest_claimable_age_seconds` | Always | Age of the source's oldest claimable outstanding reducer row at read time. |
| `claimable_depth` | Always | Count of the source's claimable outstanding reducer rows at read time. |
| `engaged_since` | When slowed | When the current slowed engagement began. |
| `stuck` | When stuck | Whether the engagement outlasted `ESHU_SOURCE_BACKPRESSURE_MAX_ENGAGEMENT` with the queue still behind: still slowed, and loud. |

Rows carry only source identity and bounded queue numbers, so scoped
callers are withheld the whole section like the other process-global
aggregates. The row shape is pinned by the `SourcePacing` component in
the OpenAPI document.
