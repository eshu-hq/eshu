# Backend Conformance

`backendconformance` owns the reusable graph-backend proof harness for matrix
validation plus deterministic read/write corpora.

## Conformance flow

```mermaid
flowchart LR
    Matrix["specs/backend-conformance.v1.yaml"]
    Parser["ParseMatrix + Matrix.Validate"]
    DefaultTests["default Go tests"]
    Corpora["DefaultReadCorpus + DefaultWriteCorpus"]
    LiveScript["scripts/verify_backend_conformance_live.sh"]
    Bolt["real Bolt backend"]
    Report["case results and profile gates"]

    Matrix --> Parser
    Parser --> DefaultTests
    Corpora --> DefaultTests
    DefaultTests --> Report
    Matrix --> LiveScript
    Corpora --> LiveScript
    LiveScript --> Bolt
    Bolt --> Report
```

The default test path validates contracts without a live database. The live
script opts into the same corpora against Neo4j or NornicDB. One pair — the
value-flow cloud sink read and seed — is itself opt-in behind
`ESHU_BACKEND_CONFORMANCE_VALUE_FLOW`, and is absent from the corpora rather
than skipped when that variable is unset.

The package keeps two contracts together:

- the machine-readable backend matrix in `specs/backend-conformance.v1.yaml`
- the profile gates that track NornicDB promotion across local and production
  shapes
- the read and write corpora used to exercise `GraphQuery` and Cypher executor
  adapters, including atomic grouped writes and transaction-visibility cases

Default Go tests validate the matrix and harness without starting Neo4j or
NornicDB. `scripts/verify_backend_conformance_live.sh` turns on the opt-in live
test and runs the corpora against a real Bolt endpoint for the NornicDB and
Neo4j Compose lanes. It prints whether the value-flow pair is INCLUDED or
OMITTED before it runs, because a run without
`ESHU_BACKEND_CONFORMANCE_VALUE_FLOW` proves strictly less than one with it.

The live write corpus includes the source-local shape that matters for canonical
projection parity: repository, directory, file, function, and
`File-[:CONTAINS]->Function`. The live test runs the write corpus twice before
readback so both official backends prove the relationship stays idempotent.
