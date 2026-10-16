# NornicDB conformance enrollment for #7533

The `e2e-tests.yml` `test (nornicdb)` job owns seven Cypher live-test files in
`scripts/lib/nornic_conformance_inventory.json`. The existing conformance
script selects all nine `Test` declarations in one NornicDB-only Go invocation.
It checks source membership before execution and Go JSON events afterward:
each test must run and pass exactly once in
`github.com/eshu-hq/eshu/go/internal/storage/cypher`, the package must pass,
and a skip or failure rejects the run. The Neo4j job keeps its core conformance
path.

The live-test ledger classifies the seven files as `dedicated_ci` with runner
`backend-conformance-nornicdb`. The validator checks exact runner membership
independently of the legacy exemption count and digest. The static mirror
plants disabled, suppressed, unselected, missing, skipped, duplicate,
wrong-package, and re-pinned-demotion cases.

At source `a7623da9c98b564c35627f46fd9b79b0aa30fbb1`, all nine tests passed
a local race run on the pinned NornicDB 1.3.3 fixture: package 1.342 seconds,
process 2.367 seconds. These shared-host figures are not a speedup or scale
claim. The modified full script passed core conformance and all nine tests once
on a fresh Linux ARM64 fixture, taking 3.571 seconds. Two earlier attempts on
a reused fixture failed in the unchanged core containment readback before the
nine-test step. A bounded diagnostic confirmed the File and Function existed
when a typed relationship count returned zero, but it did not prove why. The
retained-state failure remains open. Hosted AMD64 execution is not checked.
Neither local result by itself establishes enrollment acceptance.

The semantic concurrency test retains eight repository writers. Its cleanup
ignores errors, so a green test does not prove cleanup success. The hosted job
uses a disposable graph service and tears it down. This enrollment changes no
runtime Cypher, graph write, schema, fixture, worker count, or telemetry
contract.
