# Gate Contention Diagnosis

Before labeling an intermittent failure a flake, check host load, concurrent
`make pre-pr`/golden-corpus processes, Docker pressure, and free build-cache disk
space. Live gates use fixed ports and compete for CPU and Docker I/O even with
separate ports. The live-gate mutex covers only its script and clone; it does
not coordinate other Docker-heavy gates or other clones.

Serialize live gates and benchmarks across the shared machine and hand capacity
over explicitly. Subagents must not each run `make pre-pr`. Do not terminate
another owner's gate to make room.

A failure under contention remains an observed failure; it does not by itself
establish a product defect. Preserve its run identity and artifacts, then rerun
under controlled conditions. Repeating without changing those conditions adds
no useful evidence. Passing correctness checks under load can remain useful;
timing claims require a comparable, quiet environment.

A drain timeout or dead-lettered item can result from contention. Compare the
failing and passing runs' code, topology, inputs, and machine load before
attributing the failure to reducer or queue behavior. Capture the failure's own
state before harness cleanup; a different successful run cannot explain it.
