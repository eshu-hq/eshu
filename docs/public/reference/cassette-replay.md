# Cassette And Replay Proof

Eshu replay is the deterministic proof layer for collector output, parser
fixtures, replay scenarios, and API/MCP/CLI answer shapes. It lets contributors
and maintainers prove supported behavior without provider credentials, Docker,
Postgres, or a graph backend unless the named proof gate explicitly says a live
backend is part of the contract.

Use this page when you need to author, refresh, validate, or review a replay
scenario, or when you need to understand what a green replay or cassette result
actually proves.

## The No-Provider-Key Rule

Ordinary replay proof is credential-free. A normal validation run must not read
cloud tokens, API keys, customer endpoints, private hostnames, or local ignored
configuration. A replay input that cannot run without provider keys belongs in a
credentialed refresh job, not in an ordinary proof gate.

The split is deliberate:

- **Record or refresh** may call a live provider, using operator-owned
  credentials, to capture source behavior once.
- **Replay and validation** use committed, canonical, redacted artifacts and
  must run offline with no live provider access.

If a replay misses a request, fact kind, parser fixture, or query shape, it must
fail loudly. It must not fall through to the network, silently skip a surface, or
turn an unknown artifact into a passing result.

## Contributor Conformance Flow

For collector extraction conformance, start with the public, Docker-free
conformance suite. These commands are the runnable starter path; the package
README has the implementation details.

```bash
git clone https://github.com/eshu-hq/eshu && cd eshu/go
go test ./conformance -count=1
$EDITOR conformance/testdata/starter-spec.yaml
go run ./cmd/collector-<record-capable-collector> -mode=record \
  -cassette-file=conformance/testdata/starter-cassette.json
$EDITOR conformance/observe.go
go test ./conformance -count=1
```

The first and last commands are credential-free. The `-mode=record` command is
the optional live step: it runs your collector once against your source system,
writes a canonical cassette, and does not require Postgres, NornicDB, or Docker.
Use a binary that actually implements record mode. The current in-tree pilot is
`collector-kubernetes-live`; out-of-tree collectors should substitute their own
record-capable binary after adding the same recorder seam.

After recording real collector facts, update `conformance/observe.go` so
`Observe` maps those fact kinds into the node, edge, correlation, property, and
evidence observations expected by `conformance/testdata/starter-spec.yaml`.
The starter `Observe` seam intentionally rejects unknown fact kinds; leaving it
on the neutral `starter.*` mapping makes the final conformance test fail against
any real collector cassette.

## Replay Artifact Types

| Artifact | What it proves | Where to start |
| --- | --- | --- |
| Collector cassette | A credentialed collector's emitted facts can be replayed as if the collector ran live. | `testdata/cassettes/<collector>/<recording>.json` |
| Input tape | An HTTP-backed collector can replay provider responses through its real parsing and normalization code. | `go/internal/replay/inputtape` |
| Parser fixture | A parser emits the expected facts for a committed source tree. | `go/internal/replay/parserfixture/testdata/fixtures` |
| API/MCP golden | A graph-backed HTTP or MCP read returns the expected bounded response shape. | `testdata/golden/e2e-20repo-snapshot.json` |
| CLI golden | A CLI read surface matches the same offline answer contract as shared read surfaces. | `query_shapes.cli` in the B-12 snapshot |
| Authz scoped route | In-grant and out-of-grant scoped token behavior matches the authorization catalog. | `specs/authorization-replay-coverage.v1.yaml` |
| Scenario-depth proof | A required surface has baseline plus any applicable `delta_tombstone`, `fault`, `ordering`, `crash`, or `cost` coverage. | `specs/replay-coverage-manifest.v1.yaml` |

The generated [Replay Coverage Dashboard](replay-coverage.md) shows which
surfaces and scenario-depth classes are covered, what artifact type covers each
one, and which sibling gate proves it green.

## Authoring A Scenario

1. Pick the surface key from the source-of-truth registry:
   `collector:<name>`, `read_surface:<route>`, `cli_surface:<command>`,
   `parser:<name>`, `capability:<id>`, `product_claim:<id>`, or
   `authz_family:<family>:<mode>`.
2. Choose the scenario type. Every supported surface needs `baseline`; add
   `delta_tombstone`, `fault`, `ordering`, `crash`, or `cost` only when the
   surface's behavior depends on that depth class.
3. Add or update the artifact that actually exercises the behavior. Do not add a
   manifest row that points at a future or placeholder proof.
4. Add the manifest row in `specs/replay-coverage-manifest.v1.yaml`, including
   `surface`, `scenario`, `scenario_type`, `ref`, and `proof_gate`.
5. Run the gate that proves the artifact green, then run the replay coverage
   gate so the dashboard and coverage report remain in lockstep.

The `proof_gate` field is not decoration. It names the command or CI gate that
actually runs the scenario. A manifest row without a real sibling proof is a
false green.

## Refreshing A Cassette

Refresh only when source behavior changed, a collector fact contract changed, or
a committed cassette no longer represents the intended synthetic source. Do not
refresh to hide a failing proof.

Collector record mode is symmetric with cassette replay mode only for binaries
that wire the replay recorder. Do not assume every collector supports
`-mode=record`; add the recorder seam first, use the existing credentialed
refresh workflow for supported recorders, or treat the cassette update as
blocked until a supported recorder exists.

```bash
go run ./cmd/collector-<record-capable-collector> -mode=record \
  -cassette-file=../testdata/cassettes/<collector>/<recording>.json
```

For the current in-tree pilot, that is:

```bash
go run ./cmd/collector-kubernetes-live -mode=record \
  -cassette-file=../testdata/cassettes/kuberneteslive/supply-chain-demo.json
```

Before committing a refreshed cassette:

- review the diff line by line; canonical output should make meaningful changes
  small and readable
- confirm volatile fields normalized instead of churning the whole file
- confirm secrets, tokens, private URLs, hostnames, IP addresses, and real
  account identifiers are absent
- run the proof gate named by the manifest entry
- regenerate any generated dashboard or report only through the owning gate

The credentialed refresh workflow is separate from ordinary proof. It may use
provider secrets to re-record artifacts, but the resulting PR still needs
offline replay validation before merge.

## Validation Commands

Use the smallest command that proves the touched surface, then run the local
pre-PR gate before pushing.

```bash
(cd go && go test ./conformance -count=1)
(cd go && go test ./internal/replay/... -count=1)
(cd go && go test ./cmd/replay-coverage-gate ./internal/replaycoverage -count=1)
bash scripts/test-verify-replay-coverage-gate.sh
bash scripts/verify-replay-coverage-gate.sh --blocking
make pre-pr
```

For generated replay dashboard updates, refresh through the owning test:

```bash
(cd go && go test ./cmd/replay-coverage-gate/ -update-dashboard)
```

For API/MCP/CLI golden truth, also run the golden-corpus gate commands named in
[Local Testing](local-testing.md#quick-verification-matrix).

## Advisory And Blocking Coverage

Replay coverage has two modes:

- **Advisory** reports uncovered, unresolved, or stale rows without failing the
  command. Use this for local exploration while designing a new surface.
- **Blocking** fails on uncovered, unresolved, or stale rows. CI uses blocking
  mode for supported surfaces, and local merge proof should use
  `bash scripts/verify-replay-coverage-gate.sh --blocking`.

Blocking coverage means the coverage map is complete and the named sibling gates
exist. It does not mean every possible live provider, backend, performance, or
full-corpus behavior has been exercised.

## What Replay Replaces

Replay can replace manual checks when the question is deterministic and already
represented by a committed artifact:

- collector fact-shape regression for a recorded source
- parser output for a committed fixture tree
- API/MCP/CLI response envelope shape for the B-12 snapshot corpus
- scoped-token allow/deny behavior listed in the authorization replay catalog
- schedule, fault, crash, delta/tombstone, and cost behavior that has a named
  replay scenario and proof gate
- public product or capability claims whose deterministic proof metadata points
  at committed registries, snapshots, or gates

In those cases, prefer the replay or golden gate over a manual click-through.
It is faster, repeatable, and reviewable in a PR diff.

## What Replay Does Not Replace

Replay does not replace proof whose correctness depends on live systems,
backend-specific behavior, corpus scale, or operator runtime state:

- provider credential validation, permission-hidden behavior, rate limits, or
  live API pagination
- real Postgres queue drain, lease contention, dead letters, or claim handoff
  unless the named proof gate is a live/backend gate
- NornicDB or Neo4j planner behavior, schema/index behavior, or hot-path
  performance when those are the subject of the change
- full-corpus latency, p95/p99 budgets, memory, CPU, pprof, or benchmark
  evidence
- Kubernetes, Helm, Docker Compose, hosted deployment, or remote E2E rollout
  proof
- telemetry quality for operators unless the changed path emits and validates
  the relevant metrics, spans, logs, status, or dashboards

When a PR claims one of these behaviors, name the irreducible live/backend,
scaled, or full-corpus gate in the PR evidence. Do not cite ordinary replay as a
substitute for a proof tier it cannot exercise.

## Review Checklist

Before merging a replay or cassette change, verify:

- the artifact is synthetic, redacted, canonical, and reviewable
- the source registry, manifest row, artifact, generated dashboard, and sibling
  proof gate agree
- `scenario_type` covers the actual risk depth, not only baseline existence
- API, MCP, and CLI shapes stay in parity when they share a read surface
- authz rows cover both in-grant and out-of-grant behavior when a permission
  family is in scope
- advisory language is not used to bypass a blocking supported-surface gap
- every skipped live, backend, scaled, or credentialed check is explicitly out
  of scope or routed to a tracked follow-up
