# #7033 reader-fleet query-path A/B at source 987282a15

Performance Evidence: The query-path code source `987282a151404e7ece59802f34d7a7ae782c7c71`
was compared with main `791078e81a1f0c769ddf1e1f70792adc869cf5e7` on
2026-10-04. Both APIs were built with Go 1.26.6 (binary SHA-256 values
`88ea54e976ffc40b537fb18095647861213e510e366956c1012e9635b9f40546`
and `a7f2e34ba2f57b21f2d51c932be8f88504d1873cae5edc0030c7ff7cde6a0d3f`).
The isolated fixture had PostgreSQL 18.6 with one primary and two distinct
streaming standbys, Neo4j 2026.08.1, 400 content entities, and 200 content
files. The PostgreSQL and Neo4j image IDs were
`sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873`
and `sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`.
Both APIs used the same database, indexes, 16-term request, read credentials,
total/reader connection budgets, and telemetry configuration; only the
candidate enabled the direct-member inventory under test.
The PostgreSQL row-content, `xmin`, and index-definition fingerprint was
`18d31e039b4fd536062c95e4123d3489` on the primary and both standbys before
and after the timing window. Both APIs returned `/readyz` 200.

After two warmups per variant and two four-sample base control sets, eight
balanced ABBA rounds yielded 16 full-body HTTP timings per variant:

| Source | Median | Nearest-rank p95 |
| --- | ---: | ---: |
| Main | 0.086599 s | 0.090735 s |
| Reader fleet at 987282a15 | 0.090038 s | 0.095682 s |

The candidate/base median ratio was 1.039712, a 3.97% local median cost,
not a speedup. The eight same-round ratios averaged 1.030643 with sample SD
0.060264. All 44 warmup, control, and timed requests returned HTTP 200, 25
rows, truncation flags, 16 terms, and the same canonical JSON SHA-256
`df0faf9197e63303b2caf6be1425bcc38cbbf1fa7ef1f4ba7a7e801b24a02df7`.
The frozen base control bound was 0.110218 s, with zero exceedances. Host
load1 was 0.34/0.39/0.39 at start/maximum/end on 12 CPUs. Both reader member
ordinals served 12 successful candidate attempts each; pools ended with zero
in-use connections and reservations. The final candidate's native same-lease
identity tests passed against the owned physical standby, not just a primary.
The task-owned APIs, PostgreSQL/Neo4j containers, volumes, network, and proof
worktrees were removed; unrelated containers were left running.

The retained private harness and timing artifacts have SHA-256 values
`2b0bde8de491bf1cf909a125b24f8f9ffd9b2c5f2efcf4b76f831ffb5d85f915`
and `c0dace43b34fceab1b84b75c90644d1bd7d0dcb118052d22a77e5bb9ed596b21`.
This small fixed-corpus comparison bounds the touched fleet path. It is not
an ops-qa `<1 s` acceptance measurement or proof at 100,000 repositories.
The later `e54a85fd0` change affects bootstrap qualification cleanup and its
tests, not the timed request path after `/readyz`. That commit was not rebuilt
or re-timed in this A/B; the numbers above are tied to the stated source and
binary hashes, not claimed as an exact-final-binary latency result.

Observability Evidence: Native standby tests verified that the query-start
event records the leased backend PID and TCP peer on a recording request.
The A/B checked both member-ordinal success counters and final connection and
reservation gauges. It did not verify deployed trace retention.
