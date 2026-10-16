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
The always-run ledger verifier reads the active static workflow and gate
registry. It binds the complete shell runner, including setup, CORE, the
NornicDB branch, and the closing tail; an early exit fails the static mirror.
Before importing the verifier, the ledger binds its source and CLI control
flow, so a successful early CLI exit cannot bypass source or event checks.
The E2E PR filter also includes those control files, so an edit that removes
the static guard still selects the independent conformance job.

All nine tests passed a local race run on pinned NornicDB 1.3.3: package
1.342 seconds, process 2.367 seconds. The fresh Linux ARM64 fixture ran the
whole modified conformance script successfully (core plus all nine tests) in
3.571 seconds. That run used commit `a7623da9c98b564c35627f46fd9b79b0aa30fbb1`
plus an uncommitted diff with SHA-256
`3be01d898311c15f2a0bbce9af5f9a9240b16f2018bf0c274a55c76f8b3f4764`;
the receipt records eleven source-file hashes. These shared-host figures are
not a speedup or scale claim.

On 2026-10-16, a fresh pinned Linux ARM64 fixture ran the unchanged complete
conformance script at clean commit
`b6a20257b9465c7033457184a735101d2468f368` based on main
`aa81b9f6e8b89e688d75a95b70fca0c8be8963d6`, including the independent CLI
activation check. The script exited 0 in 5.224674 seconds (5.22 seconds).
The core test ran and passed once. Strict Go events showed one run and pass for each of the nine selected declarations:
seven backend tests and two hermetic helpers. The package passed with no test
skip or failure. The source and event guards exited 0. The worktree stayed
clean at the same commit, and twelve source hashes matched before and after.
The owned container stopped; its unique volume is preserved. The local receipt
is `nornic7-current-b6a` (`start.json`, `result.json`, `runner.log`, and
`nornic-conformance.jsonl`). This is not a hosted AMD64 proof or a
retained-state diagnosis.

Two earlier reused-fixture attempts failed in the unchanged core containment
readback. In two later parent/candidate pairs,
both arms had already run all nine tests before a subsequent core readback
returned zero containment rows. Those pairs exclude the final selector switch
as a necessary cause under that history; they do not isolate the earlier five
tests or prove a graph write versus read defect. The retained-state cause
remains unproven. Hosted AMD64 execution is not checked. The hosted full
same-fixture core, nine tests, bootstrap, API health, and teardown are required
for enrollment acceptance.

The semantic concurrency test retains eight repository writers. Its cleanup
ignores errors, so a green test does not prove cleanup success. The hosted job
uses a disposable graph service and tears it down. This enrollment changes no
runtime Cypher, graph write, schema, fixture, worker count, or telemetry
contract.
