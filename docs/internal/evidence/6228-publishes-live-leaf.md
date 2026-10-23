# Publishes Live Leaf Evidence (#6228)

The publishes family ODU moves from 12 to 13 facts: one new
`package_registry.package` envelope encoded through
`factschema.EncodePackageRegistryPackage`, and a `source_run_id`
(`run-ifa-publishes-family-1`, mirroring the git collector shape) on the four
repository envelopes, which canonical projection requires on every repository
fact. The expected live set stays the exact 3-edge set; the added package
fact is decision-neutral to it.

No-Regression Evidence (#6228): `go test ./internal/ifa/familyodu/ -run
'TestPublishes' -count=1` passes on the final tree (stable-key uniqueness,
13-fact composition of 4 repositories + 6 hints + 2 versions + 1 package, and
the source-run-id carriage test). The ODU is offline fixture-encoder code
registered at catalog-seed time: it executes no Cypher, opens no graph
session, and touches no writer, projector, or reducer path, so live graph
truth moves only through the newly wired drive/assert pair, which pins the
unchanged 3-edge intent set. Baseline is the 12-fact ODU with no package fact
and no live matrix; after is the 13-fact ODU with registry row 30, the fault
trio, and a gapless 100-cell catalog. Backend is the hermetic offline replay
through the determinism gate's locally built binaries (no live backend in
this note; live determinism and fault-injection proof lands in CI on the PR).
Input shape is the fixture package-registry scope carrying the owning
repositories, the owned package, the source hints, and the versions. Row
counts are 13 envelopes in and exactly the 3 expected edges out. Telemetry
evidence is the fault-mirror exact-cover proof
(`test-verify-ifa-fault-injection.sh`: 100 cells, 8 shards) and the
shard-balance check, both green on the rebased tree.

No-Observability-Change: the change adds no metric, span, log line, queue,
lease, worker, route, or graph query. It adds one offline fixture fact, one
payload attribute mirroring the collector, and harness-only drive/assert
wiring. Operators diagnose the path through the existing fault-injection and
determinism gates, whose CI waves are the remaining required proof before
merge.
