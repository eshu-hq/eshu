# Arbiter packet — `#10450` in-tree sbom-collection removal (step 4 prep)

To: arbiter-eshu (via coordinator — this lane's toolset has no
subagent spawn, so no verdict could be obtained in-lane).
From: lane `drive/10450-in-tree-removal` (`origin/main` `7e5e91712b`).
Raw observations: `drive-10450-in-tree-removal-CENSUS.md` (same commit).

Scope of the proposed removal (emission path ONLY, per
`go/internal/collector/sbom/EXTRACTION_PREP.md` §Scope):

DELETE candidate:
- `go/cmd/collector-sbom-attestation/` (binary: main, config,
  service, tests, README/AGENTS/doc).
- `go/internal/collector/sbom/` (document/ + runtime/ + prep docs;
  prep docs may stay as historical record — Q3).
- Wiring: `Dockerfile` build line, `scripts/install-local-binaries.sh`
  line, `go/cmd/README.md` rows, `go/internal/envregistry/`
  `entries_collectors.go` + `pages.go` + `coverage_test.go` refs.
- Compose e2e: `go/internal/runtime/compose_remote_e2e*.go` refs,
  `scripts/test-e2e-remote-compose-suite.sh`,
  `scripts/lib/test-security_intelligence_release_gate-docker-compose-remote-e2e.yaml`
  service entry.
- Vuln-flow users: `scripts/lib/test-verify-remote-e2e-vulnerability-readbacks-fake-docker.sh`,
  `test-verify-remote-e2e-tfstate-warnings*.sh`,
  `docs/public/reference/security-intelligence.md` hosted-runtime note.
- Docs: `service-runtimes-collectors.md` row,
  `environment-collectors-supply-chain.md` env vars,
  `env-registry/collectors-signals.md` section (check if generated),
  `remote-e2e-runtime-state.md`, `collector-reducer-readiness.md`
  row, `telemetry/cross-service-correlation.md` row,
  `profiling-and-concurrency.md` port row, `local-binaries.md`.
- Golden corpus: `scripts/verify-golden-corpus-gate.sh`
  `collector-sbom-attestation:sbomattestation` mapping — the cassette
  replay path (`-mode=cassette`) dies with the binary (Q3).
- `specs/surface-inventory.v1.yaml` proof_gates line (then regenerate,
  never hand-edit generated outputs).

KEEP (downstream + planning stay core-owned):
- `go/internal/reducer/sbomattest/`, `go/internal/projector/sbom/`,
  query/API/MCP attachment surfaces, `go/internal/facts/supply/chain/`
  declarations (producer-record flip is a contract change — Q3),
  `go/internal/coordinator/sbom_attestation_service.go` + planner
  (in-tree planning still plans host-path items per the scopebind).

## Q1 — Is the cutover criterion met?

Evidence: window generation `component-generation:835bcf4b…` is
terminal `failed` (projection dead-letter on reducer admission,
depth ~10k); the extracted path is not serving; the in-tree worker
is silent ~23h (last completed claim 11:50:52Z, cause unknown).
Question: does the failure sit on the collection path (host
delivered twice, 13/13 facts committed both times) so that
collection stands proven and only activation/projection is owed —
or does a failed window generation fail the cutover regardless?

## Q2 — Dark-restore and residue order

Residue now: 13 host-minted facts, 1 failed generation, 1 projector
dead_letter, 2 pending claim-plane items (fleet zombie + host item).
Question: must dark restore (plus seed-row deletion and pending-item
disposition per the recorded sequence law) land before ANY deletion
— and does the dead_letter need re-drive vs purge, and by whom?

## Q3 — Exact deletion boundary

(a) Do the EXTRACTION_PREP/SHIM_MAP/ROLLBACK_DRAFT prep docs stay as
record or go with the code? (b) Does the producer record
(`sbom_attestation.go:101`, family/kind contract yamls) flip to the
satellite repo in the same change or a prior contract change?
(c) What replaces the golden-corpus cassette path the binary
provides — satellite-run cassette, or gate carve-out with its own
proof? (d) Coordinator planner/service: confirm KEEP.

## Q4 — In-tree 23h silence: outage or intended?

Planner planned the 12:25Z fleet zombie; the worker never claimed
it; nothing completed since 11:50:52Z. Question: must the lane
re-observe the in-tree producer emitting (or prove it stood down by
design) before removal — i.e. is removal of the only proven
producer while the scope has no live producer an abort-class error?

## Q5 — Reducer-depth gate and re-entry

Depth 12,809 and rising vs the `<10,000` window gate. Question: is
depth `<10,000` with the window generation active a precondition
for the removal landing too, or only for the cutover window — and
what re-census invalidates a stale removal verdict?

## Scrutinized but NOT asked (settled in-lane)

- (`scope_id`, `source_uri`) census key: proven (instance field
  empty on all 8231 rows).
- No dual authority: proven (587 in-tree completions, 2 untouched
  pending items, no item with two claimants).
- Generated surfaces in sync: proven (both regenerations byte-clean).
- Synth-zero dependency: none found in-tree (only buildx `--sbom`
  flags and test-stub "synthetic" naming); the acceptance "pin" is
  the window-pattern control, available post-removal via satellite.
- `REMOVAL_PREP.md` under `workflow-coordinator/` is the SEPARATE
  `#8875` claim-plane removal — out of scope here; this packet
  covers only the sbom emission path.
