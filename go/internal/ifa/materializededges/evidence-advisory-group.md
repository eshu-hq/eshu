# Evidence: advisory_group direct-materialization family (#10466)

The advisory_group Odù lift landed after its blocker #10475 (the winners
reader ignored provenance members) merged and proved the production
read path live. This note records what the lift proves and how.

## Claim scope

The coverage claim starts at finding-fact admission and runs
downstream: finding facts in the production writer's payload shape
(members under `payload.provenance.advisory_group_members`, encoded
through `factschema.EncodeReducerSupplyChainImpactFinding`) are
admitted as facts, rebuilt into the canonical winners read model by
the real winners maintainer rebuild, enqueued through the production
`AdvisoryGroupProjectionEnqueuer`, and projected by the real queue,
handler, `BuildGroup`, and graph writer. The claim covers the full
member union on the AdvisoryGroup node, including members no row
carries as its own CVE/advisory id — the assertion that would have
caught #10475.

The claim says nothing about the supply_chain_impact pass's
`TouchedGroupIDs` fan-out. The group ids handed to the enqueuer are
explicit, exactly as a committed pass hands them over; deriving them
from evidence is that family's own proof surface.

## No invented transport

No collector emits a followup marker for this domain, and none was
invented. The mid-drive step (`eshu-ifa enqueue-advisory-groups`)
invokes production code at its ports — the winners store's full
resweep (the maintainer's rebuild) and the production enqueuer over
the real reducer queue — mirroring the workload_dependency
maintenance-pass precedent of invoking a production pass between
drains. The seeded parity proof (`TestParityLive`) keeps its narrower
direct-drive role unchanged; this Odù is the durable end-to-end proof
above it.

## Performance and observability

No-Regression Evidence: no production hot path changed. The new verb
is test-harness-only (`go/cmd/ifa`), runs once per live cell, and
calls the existing resweep and enqueue stores unchanged; the fixture
is six finding facts, so cell cost is one extra drain cycle.

No-Observability-Change: no production instrumentation changed. The
verb prints resweep counts and enqueued groups to the gate log, which
is where an operator debugging a red cell looks first.

## Proof commands

Hermetic (run on this branch): `go test ./internal/ifa/...
./internal/cigates/ -count=1`, `bash
scripts/test-verify-ifa-determinism.sh`, `bash
scripts/test-verify-ifa-fault-injection.sh` (133 cells), the
advisory_group guard, cassette lockstep, and roster/manifest lockstep
tests. Live matrices and their results are recorded in the PR for
#10466.
