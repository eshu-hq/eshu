# #9172 tempo satellite cutover: host-image cleanup no-regression note

## Scope

Base: merge-base with `origin/main` at `f73d52b75e`; lane HEAD
`5873b15e9f` deleted the in-tree producer (`go/cmd/collector-tempo`,
`go/internal/collector/observability/tempo`) after the satellite
cutover, and this change clears the host-image leftovers: the
`Dockerfile` and `scripts/install-local-binaries.sh` build lines, the
chart Deployment image, both Compose services, the scanner
binary-contract test entry, the stale docs rows, the
`facts/observability.go` producer kind order, and the
surface-inventory overlay row (both registries regenerated).

## Regression: host image no longer builds the deleted producer

- No-Regression Evidence: the host image built `eshu-collector-tempo`
  from `go/cmd/collector-tempo` (baseline; `docker build` failed once
  the directory was deleted, the P0 this change fixes), and after this
  change the satellite serves collection with an identical contract.

The two build lines are gone and the production stage still
copies `/go-bin/` wholesale, so no other binary is affected. The chart `tempo-collector` Deployment and the Compose `collector-tempo`
worker run the Eshu host image (chart `image.repository:image.tag`) with
command `/usr/local/bin/eshu-collector-component-extension`. Collection is
seeded from the digest-pinned satellite image
`ghcr.io/eshu-hq/eshu-collector-tempo@sha256:0230e174284bb7a9831babac9b9927be05df02e8620c20e448e91dbb1ac2ecac`
via init containers running `/usr/local/bin/collector seed` (the satellite
ships only that binary; tags are refused, digest pin only). The
`ESHU_COLLECTOR_INSTANCES_JSON` tempo instance shape is unchanged
(`collector_kind: tempo`, `mode: continuous`, claim-driven). Service,
Deployment, and profile names are unchanged, and the workload stays
default-disabled in chart and Compose.

Measured at this HEAD:

- `go test ./internal/runtime/ -count=1` passes, including the Helm
  observability-collectors contract (renders the tempo Deployment on the
  host image with the component-extension command and asserts claim env)
  and the Compose preflight/render tests.
- `bash scripts/test-remote-e2e-hosted-compose-render.sh` passes
  (tempo service present under the tempo profile with the satellite
  image and unchanged env contract).
- `bash scripts/verify-compose-helm-runtime-parity.sh` passes
  (observability collectors, probes, and ServiceMonitor coverage).
- `go test ./cmd/fact-kind-registry/ -count=1` passes and
  `bash scripts/verify-fact-kind-registry.sh` reports current.
  `go test ./cmd/capability-inventory/` does not build on this host
  (environmental CGO `bsearch.h` `_Generic` failure via its
  `go/internal/query` import; CI is authoritative), so the surface-count
  reconciliation is not re-claimed here.
- `helm template` with `tempoCollector.enabled=true` renders the main
  container on the host image (`ghcr.io/eshu-hq/eshu:v0.0.3-pre-release-18`
  from `image.repository:image.tag`) with
  `command: ["/usr/local/bin/eshu-collector-component-extension"]`, and a
  `seed-tempo-primary` init container on the digest-pinned satellite image
  with `command: ["/usr/local/bin/collector", "seed", ...]`.
- `docker compose config` (tempo profile) renders `collector-tempo`
  with a host `build:` block and
  `command: [/usr/local/bin/eshu-collector-component-extension]`,
  `collector-tempo-seed` on the digest-pinned satellite image with the
  `collector seed` command and no `build:` block, and
  `collector-tempo-reconcile` on a host `build:` block.

Coordinator claim planning (`planner/tempo`), reducer projection, and
query surfaces are untouched, so in-tree collection semantics cannot
regress: the only in-tree behavior change is the absence of a binary
nothing else in the image referenced.

## Observability: no new or altered telemetry

- No-Observability-Change: this change adds no metrics, spans, logs,
  or status fields. The Go edits delete the in-tree producer package and
its tests, drop the tempo fault-table row and the encode-adoption ratchet
entry, reorder a producer kind list, and pin the Helm contract to the
host-image-plus-seed shape; no coordinator, planner, reducer, or query
logic changes. Chart and Compose edits change image references, not
probes, ports, or metric wiring (parity script confirms probes and
ServiceMonitor coverage). Collection telemetry now comes from the seeded
satellite binary, which is the extracted identical code (byte-identical
parity proven by the #8867 extraction-prep mirror tests before the
mirror files were deleted with the producer); operator-visible signals
(`/healthz`, `/readyz`, `/metrics`, `/admin/status`) keep the same
paths and ports.
