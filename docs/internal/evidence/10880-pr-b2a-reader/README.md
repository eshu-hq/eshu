# #10880 PR-B2a reader evidence: product statements on the 8M-support corpus

Refs #10880. Timings of the statements the reader PR ships, run through the real
stores (not the theory-proof statements), on the corpus the theory proof built
(`docs/internal/evidence/10880-pr-b2a/`, migration PR). Synthetic data only.

## Decision, threshold, deadline (stated before measuring)

- **Decision.** Whether the shipped unscoped existence-filter reads meet the
  ruling's bar on the product schema: every read under 1,000 ms warm, and the
  every-winner-fails case under 1,500 ms.
- **Threshold.** Warm is the median of five runs after one warm-up. Reads: list
  (status, sorts, rank and suppression filters, cursor, one CVE, the 15,000-key
  axios package, a repository, a service filter), count, the five inventory
  dimensions, grouped inventory (service, workload, environment), and the image
  reads, with and without 6 KB payloads.
- **Deadline.** 2026-11-23T12:30Z. Started 11:45Z.

## Files

| File | What it is |
| --- | --- |
| [RESULTS.md](RESULTS.md) | The numbers, the limits, and `NOT_CHECKED`. |
| `results_product_narrow_payload.tsv` | Median and max ms per read, 200-byte payloads. |
| `results_product_wide_payload.tsv` | The same reads with 6,250-byte incompressible payloads on every winners row and every key's top support. |
| `results_product_worst_case.tsv` | Every one of the 80,001 winners fails precise (64,001 candidates after the status prune), wide payloads. |
| `results_hot_supports_defined.log`, `results_hot_many_keys_list.log`, `results_hot_many_keys_count_inventory.log` | Hot-key timings: the 8M corpus with a hot package and a hot CVE, and the 2.6M-key corpus. |
| `hot_key_plan_*.txt` | Raw `auto_explain` plans of the V statements on the 2.6M-key corpus (list: no selector, package, CVE, CVE and package; count: no selector, package). |
| `results_jit_steady_state.log`, `results_jit_custom_plan.log`, `hot_and_jit_harness.go.txt` | The JIT 20 % measurement and its harness. |
| `product_timing_harness.go.txt` | The Go harness that produced the rows. It is a `.txt` file: it needs a database on the supports schema and runtime facts the harness seeds, and is not a test the repository runs. |
