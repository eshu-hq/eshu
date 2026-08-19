# AGENTS.md — internal/backendconformance guidance for LLM assistants

## Read first

1. `go/internal/backendconformance/README.md` — pipeline position, the two
   contracts the package keeps together (matrix + corpora), and the live-test
   opt-in.
2. `go/internal/backendconformance/doc.go` — the package contract anchor.
3. `go/internal/backendconformance/matrix.go` — `Matrix`, `Backend`,
   `CapabilityEntry`, `Classification` and `CapabilityStatus` enums; the
   structural shape of the YAML in `specs/backend-conformance.v1.yaml`.
4. `go/internal/backendconformance/corpus.go` — `ReadCase`, `WriteCase`,
   `Report`, and the `RunReadCorpus` / `RunWriteCorpus` /
   `RunPhaseWriteCorpus` runners that drive `GraphQuery` and the
   `sourcecypher.Executor` seam.
5. `go/internal/backendconformance/profile_matrix.go` — `ProfileGate`,
   `ProfileRemaining`, and the promotion gates that track NornicDB across
   local and production shapes.
6. `specs/backend-conformance.v1.yaml` — the source-of-truth matrix the
   package parses; changes here must travel with `matrix.go` updates.

## Invariants this package enforces

- **Default test path is deterministic and live-database-free** — the
  in-tree default tests read directly from the corpus structures and must
  not require Neo4j or NornicDB. Live tests live behind the `live` build
  tag and the `scripts/verify_backend_conformance_live.sh` opt-in.
- **`Matrix` is a parsed view of the YAML spec** — `ParseMatrix` is the
  only path from raw bytes to `Matrix`; tests reach in via
  `ParseCapabilityMatrixBackendIDs` for backend-id coverage checks. Do
  not hand-construct `Matrix` values in non-test code.
- **`ReadCase` and `WriteCase` are the unit of behavior** — runners
  iterate cases and accumulate a `Report` (a `CaseResult` per case) so a
  single failed case does not stop the run.
- **`PhaseGroupExecutor` is the phased-write seam** — `RunPhaseWriteCorpus`
  drives that surface; do not bypass it from caller-side wiring when a
  phase boundary matters.

## Common changes and how to scope them

- **Add a new read case** → append to `DefaultReadCorpus` in `corpus.go`,
  add the matching expected shape in the same file, run
  `go test ./internal/backendconformance -count=1`. Live tests will pick
  up the new case automatically through `RunReadCorpus`.

- **Add a new write case** → append to `DefaultWriteCorpus` in
  `corpus.go`, run the local default tests, then run the live opt-in via
  `scripts/verify_backend_conformance_live.sh` against both Neo4j and
  NornicDB Compose lanes. Add `ESHU_BACKEND_CONFORMANCE_VALUE_FLOW=1` to
  that run when you need the value-flow pair too; it is absent from the
  corpora without it, so a green run does not cover it. **Add a matching retract to `cleanupLiveCorpus`
  in `live_test.go` in the same change** — a write case with no cleanup
  leaks its fixtures permanently on a persistent developer database.

- **Both corpora are `append(...)` calls**, so a related read/write pair
  that is large or has its own rationale can live in its own
  `corpus_<name>.go` and be appended by a helper, as
  `corpus_value_flow.go` does. Prefer that once inlining would push
  `corpus.go` past the 500-line cap, or once the pair needs more comment
  than case.

- **A case that reproduces a backend defect** should run the *whole*
  production statement rather than stopping at the first clause that
  diverges. A case truncated at the first known bug goes green the moment
  that one bug is fixed, while the real query stays broken on a later
  clause. **Pin it by equality to the production constant, not by a list of
  fragments.** A fragment list bounds only the mutations someone thought of;
  the one here was defeated three separate times — by decomposing a multi-hop
  MATCH, by dropping a `WHERE size(...) = 1` filter, and by truncating the
  `RETURN` — each of which keeps the case name and `MinRows` and still returns
  a row on a conforming backend. See
  `TestValueFlowReadCaseEqualsTheProductionStatement` for the shape. Mind the
  import direction: this package may import `internal/reducer` — it already
  does, transitively, through `internal/storage/cypher` — so the equality lives
  here and the production constant is exported. The reverse, a reducer-side test
  importing this package, IS a cycle.

- **Add or change a backend capability** → update
  `specs/backend-conformance.v1.yaml`, then update the
  `Classification` / `CapabilityClass` / `CapabilityStatus` enums in
  `matrix.go` if the spec introduces a new value, then run the matrix
  tests.

- **Add a new profile gate** → extend `RequiredProfileMatrixProfiles` in
  `profile_matrix.go`, declare the gate's expected verification shape,
  and update the consuming compose / runtime tests that assert on it.

## Failure modes and how to debug

- Symptom: `go test ./internal/backendconformance` fails on `Matrix`
  parsing → likely cause: `specs/backend-conformance.v1.yaml` and
  `matrix.go` enums diverged → check the new YAML value lands in the
  matching `Classification` / `CapabilityClass` / `CapabilityStatus`
  enum.

- Symptom: `RunWriteCorpus` returns a `CaseResult` with mismatched
  expectations → likely cause: the executor adapter under test does not
  match the `WriteCase` expectations → inspect the case's expected
  payload and the actual `sourcecypher.Executor` write behavior.

- Symptom: live test passes locally but fails in Compose lane → likely
  cause: phased-write ordering differs under the `PhaseGroupExecutor`
  → run `RunPhaseWriteCorpus` against both lanes and compare reports.

## Anti-patterns specific to this package

- **Adding live-database calls to default tests** — keeps CI fast and
  deterministic; live work belongs behind the `live` build tag and the
  opt-in script.
- **Hand-writing a `Matrix` literal in production code** — every parsed
  matrix must come through `ParseMatrix` so the YAML stays the source of
  truth.
- **Skipping the report aggregation** — runners accumulate every case so
  a single failure does not hide the rest. Bailing on the first error
  loses coverage data the report consumers depend on.

## What NOT to change without an ADR

- The matrix spec format
  (`specs/backend-conformance.v1.yaml` + the v1 contract): version it
  rather than mutating in place. See
  `docs/public/reference/graph-backend-operations.md`
  for the chunked rollout plan and the matrix's place in it.
- The default corpus contents: cases are referenced by tests outside this
  package; renames or removals need a coordinated update of every caller.
