# scanner-worker Agent Notes

- Keep this binary focused on claim-driven scanner-worker execution.
- Scanner workers emit source facts only. Do not add reducer finding admission,
  graph projection, or query truth here.
- Keep retry, dead-letter, metric, and log payloads bounded. Raw repository
  paths, image names, registry URLs, package coordinates, and bucket keys must
  not appear in labels or failure messages.
- Reuse `ESHU_PPROF_ADDR` for profiling and keep pprof private.
- Any concrete analyzer adapter must prove resource-limit handling, retries,
  dead letters, fact output validation, CPU, memory, queue age, duration,
  target count, and result count before it becomes a default.
