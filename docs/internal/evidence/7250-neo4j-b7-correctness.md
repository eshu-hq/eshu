# #7250 full Neo4j corpus correctness proof

## Scope and source

On 2026-10-01 the full B-7 gate completed on reviewed source commit
`5af57e21c77ffdaa03847f7902988ad60967b003`, tree
`e4cafadd837c0a61862175c990e2a85e9c73389b`. Later documentation-only changes do
not change the executed code; this result remains attributed to that commit.

The run used a dedicated Linux amd64 machine with 16 logical CPUs and nominal
128 GiB RAM, Neo4j 2026 Community and Postgres. It used fresh database volumes,
rebuilt source binaries, default concurrent projector/reducer settings, the
complete fixture corpus, and all three maintenance passes. Owned warm compiler
and module caches were reused; this is not cold-start performance evidence.
A private adapter changed only API/MCP listen addresses to loopback and executed
the rebuilt binaries with their original arguments.

The original attempt failed before database creation because the private
runner checked a command-substitution child PID against the parent lock
identity. A separately reviewed correction and explicitly authorized additional
attempt produced the result below. The failed attempt is retained separately.
No further attempt or timing-baseline change is implied by this record.

## Observed result

| Check | Result |
| --- | --- |
| Main graph/query/timing assertions | 572 pass, zero required failures, three advisory warnings |
| Corpus | 31 repositories, 176 required graph correlations |
| Read surfaces | 74 HTTP cases and 170 MCP cases passed |
| Initial and three maintenance drains | Zero residual work and nonterminal intents/events; all required checks passed |
| Gate pipeline metric | 521 seconds (8m41s), within the 1,800-second ceiling |
| Whole controller interval | 671 seconds (11m11s), including setup/build/cleanup |
| Gate, controller, cleanup exits | All zero |
| Full host-log preservation | Copy and cleanup receipts zero; 83 evidence files retained and hashed |
| Independent cleanup observation | No owned processes, containers, volumes, networks, listeners, lock or reservation remained |

The corpus contained 166 File nodes. It does **not** exercise the 5,000/5,001
workflow evidence boundary. The separate failing-to-passing boundary,
real-dispatch HTTP/MCP and story regressions in
[the coverage evidence](7250-capped-workflow-coverage.md) supply that proof.
B-7 complements those checks by exercising the real pipeline and the new
run-correlation contradiction assertions over HTTP and MCP.

## Timing and resource limits

Three advisory phase warnings were preserved: bootstrap 12 seconds versus a
historical 5 seconds, first drain 190 versus 75, and maintenance drains 141
versus 25. The committed historical baseline uses 20 fixtures, NornicDB and an
Apple Silicon laptop. This run uses 31 repositories, Neo4j and Linux. These
phases are not a comparable speedup or regression measurement. Passing the
pipeline ceiling is not endpoint latency acceptance.

Pressure sampling captured 648 points with no adjacent gap above two seconds:
maximum load1 6.66, minimum available memory 85,924,964 KiB, and no swap use.
Owned process/database resource sampling captured 158 points with no gap above
five seconds and peak 5,735,505,920 bytes. These remained below the run's load1
8 and owned-memory 32 GiB stop limits, with at least 32 GiB host memory available.
Independent parsing checked the raw series, not only their summaries.

## Artifact identities

Raw logs and private runner receipts are retained in operator-local evidence;
they are not committed here. The coordinator and independent teammate checked
the full file custody manifest and executed assertion census.

| Artifact | SHA-256 |
| --- | --- |
| Full gate log | `4160abc46387d230108a8ddb517e9fbc1f52e1e97645bf6bc1e63a8de0a1098e` |
| Corpus snapshot at tested source | `059a12cc394abf039d4d3e981c53ce587a490e18942d8609053c38396bede0d7` |
| API binary observed at listener | `65de1416504f83b835c4b7fe4cef05152c4d85648497fe3e7bd7802fc2169785` |
| MCP binary observed at listener | `de4810dc6f46feb5553ff1bc3e842a3a240beff438bd06a1d7da9fcb1f53f40d` |
| Postgres image manifest | `d3e1620b530c944afa6e887d22eb899824da68e19c52024bf98f5220c88a65b2` |
| Neo4j image manifest | `eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f` |

## Remaining acceptance

This establishes full-corpus functional proof for the correctness patch. It
establishes no latency fix, deployed response parity, representative write-cost
comparison, independent cold/warm endpoint p95, hosted CI or issue closure.
At the time of this run, PR creation and PostgreSQL scale CI were on hold.
Under the resumed seven-issue goal, normal publication and required hosted CI
are authorized. Final review, attestation and the local push floor remain
required.
#7250 stays open until its measured latency fix and deployed acceptance pass.
