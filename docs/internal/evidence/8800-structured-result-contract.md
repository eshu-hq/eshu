# Structured MCP result contract proof (#8800)

The structured Neo4j golden-corpus run passed 629 checks with zero required
failures. This records the local pipeline and query assertions for the runtime source
below. Review subsequently found that the reader could accept legacy fallback
results despite requesting structured mode; a stricter reader and fresh run are
required before claiming the requested wire contract. It does not establish
hosted token savings or a default flip.

## Run identity

| Field | Value |
|---|---|
| Tested commit | `635a19daadba81ed6f72f0a9bf8f2d1ea6dc5c03` |
| Base | `7d0cced37317c5d96a037fd7f30474dd05dc44d7` |
| Go tree | `91ff739d65770b771c5e0f2ad55b26ee16165f5e` |
| Toolchain | Go 1.26.10, linux/arm64 |
| Substrate | Docker 29.1.3; Compose 2.40.3+ds1-0ubuntu1; pinned Neo4j compose image |
| Profile and storage | `local_full_stack`; fresh isolated Postgres and Neo4j volumes |
| Corpus | 35 staged repositories; 19 cassette collectors; 40 replay generations |
| Result | 629 pass, 0 required failures, 2 advisory timing warnings |
| Reported pipeline duration | 357 seconds (5 minutes 57 seconds), ceiling 1,800 seconds |
| Raw log SHA-256 | `878fec2a1faac693a0c1a37b62a3247fb04555352b9327184a2c7b761b509b5a` |

The command was bounded by a 2,400-second timeout and used a unique compose
project and seven verified-free host ports. Its test command is:

```bash
GATE_MCP_RESULT_SHAPE=structured ESHU_GRAPH_BACKEND=neo4j   bash scripts/verify-golden-corpus-gate.sh
```

The wrapper exited 0. Cleanup left no containers belonging to its compose
project. The original evidence-recording commit changed documentation only. The later
reader correction requires object `structuredContent` and rejects embedded
resources on explicitly structured runs before checking either successes or
refusals. The actual-consumer regression failed before this correction. Fresh
strict-reader corpus proof is pending; the run below remains historical evidence.

## Required and advisory findings

The first drain and every maintenance drain reached zero residual fact work,
required projection intents, completion events and unroutable intents, with no
dead letters. Graph and HTTP/MCP query assertions passed without snapshot edits.

The warnings were `phase_first_drain` (129 seconds; legacy advisory ceiling
86.2) and `phase_maintenance_drains` (200 seconds; ceiling 30). The earlier
accepted structured run at `d41890a745afa25404932166cee70516bb09fcae` had the
same warning classes at 127 and 258 seconds, and reported 433 seconds overall.
These are observations, not an attributed speedup: the source and toolchain
changed between runs. Neither timing baseline nor assertion was relaxed.

Root-Cause Evidence: the first changed-code run at `9bce222b46` failed three
expected-refusal assertions because the golden reader matched the bounded human
summary instead of `structuredContent.error.message`. Its actual-reader tests
were RED before the consumer correction and GREEN afterward. The corrected
reader rejects malformed canonical error envelopes, stale text and protocol or
transport errors as substitutes for a tool refusal. Snapshot expectations stay
unchanged.

## Focused proof and performance scope

The MCP and golden-reader suites, their focused race checks, vet, changed-package
lint, strict documentation build and required deferred gates passed. Tests cover
non-object JSON and text, null and numeric precision, repeated rendering,
object-key collisions, direct/routed calls, exact byte boundaries, unchanged
legacy bytes, and bounded unknown-tool labels and logs.

A same-host HTTP-500 call-and-marshal microbenchmark used three 250 ms samples
per state. Median cost rose from 3,927 ns to 5,391 ns (+1,464 ns, 37.3%);
allocations rose from 65 to 74. The changed path constructs and budgets the
structured error envelope. Common renderer samples stayed within about 3%;
normal full dispatch was not separately measured. This is a scoped cost
measurement, not an end-to-end no-regression or token-saving claim.

No-Observability-Change: the golden-reader correction adds no platform signal.
The runtime change deliberately adds structured generic failures to the
response-bytes population and normalizes unknown names; the telemetry reference
documents that change. The dispatcher counter still records one ending per call.

## Remaining evidence

The two-client three-size/control token comparison, paired scoring, other-client
manual checks and one week of hosted observation remain unproved. The separate
production-class 14-day dispatch-counter window remains in #8799. Keep `dual`
as the process default until the recorded acceptance conditions are satisfied.
