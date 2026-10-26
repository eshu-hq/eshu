# Structured MCP result contract proof (#8800)

The strict structured Neo4j golden-corpus run passed 629 checks with zero
required failures. This proves the local pipeline and requested result contract
for the source below; it does not establish hosted token savings or a default flip.

## Run identity

| Field | Value |
|---|---|
| Tested commit | `9a43126e3f56aeb2c53814b9e6da913374619b5c` |
| Base | `7d0cced37317c5d96a037fd7f30474dd05dc44d7` |
| Go tree | `d7f8599a2989f8b604c853297ec80d0c2fffaf6a` |
| Toolchain | Go 1.26.10, linux/arm64 |
| Substrate | Docker 29.1.3; Compose 2.40.3+ds1-0ubuntu1; pinned Neo4j compose image |
| Profile and storage | `local_full_stack`; fresh isolated Postgres and Neo4j volumes |
| Corpus | 35 staged repositories; 19 cassette collectors; 40 replay generations |
| Result | 629 pass, 0 required failures, 2 advisory timing warnings |
| Reported pipeline duration | 436 seconds (7 minutes 16 seconds), ceiling 1,800 seconds |
| Raw log SHA-256 | `6844f901701ebe4eb01e9ddc4198d9ec40117c7582943bc9e06b593ff7f861b0` |

The command was bounded by a 2,400-second timeout and used a unique compose
project and seven verified-free host ports. Its test command is:

```bash
GATE_MCP_RESULT_SHAPE=structured ESHU_GRAPH_BACKEND=neo4j bash scripts/verify-golden-corpus-gate.sh
```

The wrapper exited 0. Cleanup left no containers belonging to its compose
project. The final evidence-recording commit changes documentation only; the
tested Go tree, gate scripts, corpus, snapshots and specifications are unchanged.

## Required and advisory findings

The first drain and every maintenance drain reached zero residual fact work,
required projection intents, completion events and unroutable intents, with no
dead letters. Graph and HTTP/MCP query assertions passed without snapshot edits.

The warnings were `phase_first_drain` (189 seconds; legacy advisory ceiling
86.2) and `phase_maintenance_drains` (200 seconds; ceiling 30). The earlier
accepted structured run at `d41890a745afa25404932166cee70516bb09fcae` had the
same warning classes at 127 and 258 seconds, and reported 433 seconds overall.
These are observations, not an attributed speedup: source and toolchain changed.
Neither timing baseline nor assertion was relaxed.

Root-Cause Evidence: the first changed-code run at `9bce222b46` failed three
expected-refusal assertions because the reader matched the human summary
instead of `structuredContent.error.message`. Its actual-reader tests were RED
before correction and GREEN afterward. The subsequent `635a19daad` run passed,
but review found it could still accept legacy text/resource fallback when
structured mode was explicitly requested. That run alone was insufficient proof
of the requested wire shape.

The corrected reader now requires object `structuredContent` and rejects
embedded resources before either success or refusal assertions in explicitly
structured runs. A 36-case actual-consumer regression was RED before this fix
and GREEN afterward. Dual and unspecified modes retain legacy fallbacks.
Malformed machine errors and transport/protocol failures cannot satisfy an
expected tool refusal. The final live run above used this stricter reader.

## Focused proof and performance scope

The MCP and golden-reader suites, focused race checks, vet, changed-package lint
and strict documentation build passed. Tests cover non-object JSON and text,
null and numeric precision, repeated rendering, object-key collisions,
direct/routed calls, exact byte boundaries, unchanged legacy bytes, and bounded
unknown-tool labels and logs.

A same-host HTTP-500 call-and-marshal microbenchmark used three 250 ms samples
per state. Median cost rose from 3,927 ns to 5,391 ns (+1,464 ns, 37.3%);
allocations rose from 65 to 74. The changed path constructs and budgets the
structured error envelope. Common renderer samples stayed within about 3%.

A separate successful canonical dispatch-and-marshal benchmark used the same
fixture, Go 1.26.10, one CPU, and five 500 ms samples per state. Baseline
`7cdb028ef135` has the same MCP source and module as the pinned base; candidate
was `9a43126e3f`. A private Go build overlay supplied the identical benchmark
without changing either checkout. Both emitted identical per-shape wire hashes.

| Shape | Baseline median | Candidate median | Change | Allocations | Bytes per operation |
|---|---:|---:|---:|---:|---:|
| dual | 19,976 ns | 19,981 ns | +0.03% | 163 in both | 11,536 to 11,584 |
| structured | 15,811 ns | 15,625 ns | -1.18% | 137 in both | 8,944 to 8,992 |

Both medians meet the predeclared 5% threshold. These are scoped measurements,
not end-to-end speedup or token-saving claims; the smaller structured median
is not an attributed improvement.

No-Observability-Change: the golden-reader correction adds no platform signal.
The runtime change deliberately adds structured generic failures to the
response-bytes population and normalizes unknown names; the telemetry reference
documents that change. The dispatcher counter still records one ending per call.

## Remaining evidence

The two-client three-size/control token comparison, paired scoring, other-client
manual checks and one week of hosted observation remain unproved. The separate
production-class 14-day dispatch-counter window remains in #8799. Keep `dual`
as the process default until the recorded acceptance conditions are satisfied.
