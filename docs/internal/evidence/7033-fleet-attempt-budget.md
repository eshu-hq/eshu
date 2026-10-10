# #7033 fleet attempt-budget proof

Performance Evidence: The fixed 100 ms request-time setup attempt was replaced
with a share of the *remaining* replay deadline for each eligible, untried
member. This changes only the opt-in physical-reader fleet. A production
one-reader QA API scrape (image `sha-306eac0`) found 3,647 of 3,974
successful `reader_replay` stages at or below 100 ms: 327 (8.23%) exceeded
that limit. Replay is only one part of four-connection snapshot setup; this
metric rejects the old cap but does not estimate full fleet setup latency.

The before source was `549146b02d07c13070149683136d099afae4ceb2`;
the candidate source was `aaaf47ff796e5d03bf20b6e7b830c8107c41977f`.
Both test binaries used Go 1.26.6 and the identical
`go/internal/runtime/postgres/reader_fleet_bench_test.go` source
(SHA-256 `cb56b6e7fc7ca7b43929cd41b45d58616bda89ec7d947d03ebde6e5fd3a0e8da`).
Binary SHA-256 values were
`80e266e04b9190dc6adfe33c2b1c54ce3581a72e008747f690988289e603955a`
and
`de8a0b1f95da955337ed50e570d7fcf3b14c6714be55ee59f1332d528b04f580`.
The owned fixture used PostgreSQL 18.6 primary plus two streaming physical
standbys, all on pinned image
`sha256:77f585114c32fbca283dc835b0596f4e52b51b4c6662d7810b2f4084f60a1873`.
It ran on an Intel Core i7-8700K (12 logical CPUs, 62 GiB RAM). All three
databases had the same catalog fingerprint
`a560357e020e01830fffba752d8c1edd` before and after both timing windows;
the primary reported two streaming standbys. Both variants used the same
database, network, pool limits, replay window (2 s), and fixture state.

Each profile had one warmup per variant, then eight interleaved ABBA rounds:
16 observations per variant. The healthy profile used eight complete
four-connection snapshot setups per observation; the deliberately stalled
first-member profile used one. The latter benchmark verified that each sample
reached the injected stalled `BEGIN`, switched to the second physical member,
and left no in-use connection or reservation. Units below are seconds per
complete setup, including cleanup.

| Profile | Before median / p95 | After median / p95 | Interpretation |
| --- | ---: | ---: | --- |
| Healthy | 0.007061872 / 0.007399960 | 0.007069408 / 0.007500384 | +0.11% median, effectively unchanged on this fixture |
| First member stalled | 0.128488742 / 0.130781968 | 1.029627143 / 1.032172251 | +0.901138401 s median; degraded failover exceeds 1 s |

The degraded increase is the deliberate cost of giving a healthy but slow
member up to half the shared two-second window. It is **not** a speedup or a
subsecond failover result. The earlier 16-term endpoint A/B on a 400-entity,
200-file corpus is recorded in
[the reader-fleet query-path proof](7033-reader-fleet-identity-final-source.md);
these setup microbenchmarks use the same small administrative test database on
both variants and do **not** prove a deployed QA endpoint or 100,000-repo
latency. #7033 remains open for a deployed two-reader endpoint measurement.

Healthy raw ns/op observations:

| Round | Before 1 | Before 2 | After 1 | After 2 |
| --- | ---: | ---: | ---: | ---: |
| 1 | 7167052 | 6994682 | 7155704 | 7045106 |
| 2 | 7399960 | 7358452 | 7361092 | 6896713 |
| 3 | 7046204 | 7081091 | 7024423 | 7250652 |
| 4 | 6874452 | 6950905 | 7066276 | 7500384 |
| 5 | 7145759 | 7077539 | 7072540 | 7170232 |
| 6 | 7258257 | 6956071 | 6968838 | 6978433 |
| 7 | 6888166 | 7110145 | 7132229 | 7064384 |
| 8 | 6894713 | 6898538 | 6919803 | 7378334 |

Stalled-first-member raw ns/op observations:

| Round | Before 1 | Before 2 | After 1 | After 2 |
| --- | ---: | ---: | ---: | ---: |
| 1 | 128840869 | 129040736 | 1028854579 | 1029293089 |
| 2 | 129168459 | 127457307 | 1029504944 | 1029483425 |
| 3 | 128234331 | 128162104 | 1030166558 | 1028681049 |
| 4 | 128407859 | 128026230 | 1029678926 | 1029575359 |
| 5 | 128496241 | 128481242 | 1029930981 | 1030614509 |
| 6 | 128680370 | 128909253 | 1029255214 | 1030267830 |
| 7 | 127681777 | 130781968 | 1029865807 | 1028240449 |
| 8 | 128509794 | 127248390 | 1032172251 | 1030108410 |

No-Regression Evidence: The fixed-policy regression was RED on both a
150 ms single-stage setup and four sequential 40 ms setup stages: the old
100 ms cap switched to reader 1. Both are GREEN with fair shares. The
`TestFleet*` suite passed normally and with `-race` on the owned physical
standbys, including stalled BEGIN/export/import, replay-lag fallback, returned
snapshot usability, and reservation cleanup. The no-fixture full
`internal/runtime/postgres` package and focused code-topic query tests also
passed normally and with `-race`.

Observability Evidence: The existing bounded reader-stage histogram,
member-attempt counter, and member pool/reservation gauges remain in place.
The A/B benchmark checked zero final reservations and in-use connections;
it did not validate deployed trace retention.
