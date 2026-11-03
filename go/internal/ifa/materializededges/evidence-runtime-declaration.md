# Runtime Declaration Family Evidence (#6228)

No-Regression Evidence: the lane declares `runtime_declaration` fault
trio cells 122-124 with one ODU, one committed cassette, and registry
rows; the only production `.go` touch in the fix commit stamps the two
asserted properties (`raw_spec`, `spec_kind`) onto the matched
`DECLARES_RUNTIME` edge, mirroring `NewKubernetesCorrelationEdgeWriter`
and `NewCodeTaintEdgeWriter`. Same UNWIND batch, same 4 edges, same
MERGE keys — write volume and statement shape are unchanged apart from
two SET clauses on the already-bound relationship. Before/after proof on
the pinned backend: pre-fix replay on scratch NornicDB showed 4 missing
+ 4 asserted-property defects; post-fix replay shows 4 exact matches on
the same 4-row statement.

- Baseline: origin/main 66588b6097 (family waived: no ODU, fixture, or
  registered edge types).
- After: `go test ./internal/ifa/...` green;
  `test-verify-ifa-fault-injection` mirror 124 cells / 8 shards green;
  live determinism N=1,2,4 digest
  `738e5debfb29c655382b7dcf87e862928b9f6dab2495620ac9593f192979d871`
  (walls 254s/279s/218s); live fault shard 2/8 EXIT=0 with the trio
  converging to baseline digest
  `6d5fe7275d95a9ac6171592900d7670278200e24cfe4ce3d617cd4d870a44763`
  (baseline wall n/a; killworker 76s; failgraphwrite 77s).
- Backend/version: gate-owned disposable Compose stack (Postgres +
  NornicDB v1.3.3
  `ghcr.io/eshu-hq/nornicdb-amd64-cpu:fix-500-e022384c@sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1`).
- Input shape: runtime ODU (repository + 3 `DECLARES_RUNTIME` lanes);
  4-edge expected set on the fixed cassette identity
  (`local_path /repo-runtime-pins`,
  `source_run_id run-ifa-runtime-declaration-family-1`).
- Terminal counts: determinism residual 0, intents 0, completion 0;
  fault shard 2/8 all cells pass, trio digests equal baseline.

Observability Evidence: no spans, metrics, logs, ports, or status fields
added. Family failures report through the existing gate signals
(assert-edges mismatch output, reducer `declaration_count` log).
