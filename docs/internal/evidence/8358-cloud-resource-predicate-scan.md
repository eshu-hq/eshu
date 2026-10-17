# 8358 cloud_resource predicate-scoped assert-edges: route proof and no-regression evidence

## Decision

Route 2 (predicate-scoped scan), per the #6228 arbiter verdict's follow-up
options in #8358. Route 1 (close the vocabulary upstream) was measured
first and rejected on the census below.

## Theory proof (before implementation)

Collector-constant census over `go/internal/collector/{awscloud,gcpcloud}`
(non-test Go, `Relationship*` / `relationshipType*` string constants):

- AWS: 702 distinct `relationship_type` verb constants.
- GCP: 161 distinct verb constants.
- Azure: 1 (`managed_by`, already closed and screened in the writer).

Closing route 1 would therefore need an 864-type registry set, an
864-alternative retract for the AWS/GCP writers, an expected-edge fixture
naming every registered type, and a factschema narrowing (a major contract
change) that rejects fixture-source and external-collector verbs the
writers accept today. That trades graph accuracy for registry shape — the
outcome the verdict rejected. Route 2 needs no vocabulary closure: the
scan scopes by the conjunction the writers already guarantee (type prefix
+ evidence_source + CloudResource endpoints, same-writer pairing).

Exactness (predicate set == retract set) rests on the sole-creator census
in `go/internal/ifa/materializededges/cloud_resource_census_test.go`: the
three prefix literals and three evidence_source stamps have exactly one
creating site each (plus named read-side consumers), and
`WriteCloudResourceEdges` has exactly three implementations. A seeded
second stamp definition turned the census red; removing it returned green.

## No-Regression Evidence: production Cypher is byte-identical

Baseline: `origin/main` writer templates. After: this branch. Input shape:
the nine production templates of the three writers (one upsert format +
two retracts each). Backend/version: not applicable — no statement text
reaches a backend differently; the comparison is over the exact strings
the writers execute.

Measured by extracting each raw-string template literal from both trees
and comparing bytes: all nine (`canonical*CloudResourceEdgeUpsertCypherFormat`,
`retract*CloudResourceEdgesCypher`, `retract*CloudResourceEdgesByUIDsCypher`
for AWS, GCP, Azure) are IDENTICAL. The only writer-code changes are
exported alias consts and replacing the `"AWS_"`/`"GCP_"`/`"AZURE_"`
literals with the new exported prefix consts of the same value, so
statement text, parameters, batching, grouping, and queue/lease behaviour
are unchanged by construction. Terminal queue or row counts: unchanged —
no reducer, projector, or queue code is touched. The 143 pre-existing
`CloudResource` writer tests in `go/internal/storage/cypher` (which assert
exact statement text) pass unchanged.

The predicate itself is evaluated client-side over the `eshu-ifa`
graph dump stream in `go/cmd/ifa/assert_edges_scan.go`; no Cypher
`type(rel) STARTS WITH` predicate is sent to Neo4j or NornicDB (the pinned
NornicDB build mis-evaluates `STARTS WITH` in multi-node MATCHes, #6546),
so there is no new backend query shape to measure. `assert-edges` already
streams every edge for finite-set families; the predicate branch adds one
prefix check and one string compare per edge on that existing stream.

## No-Observability-Change: no new operator signal surface

No metric, span, log key, or status field is added or renamed. The change
is test-gate and registry surface only: the writers' existing statement
metadata (phase, entity label, summary) is untouched, and the new
observable behaviour is the `eshu-ifa assert-edges -domain cloud_resource`
verdict line in the Ifá gates, which is the proof surface this issue
exists to add.
