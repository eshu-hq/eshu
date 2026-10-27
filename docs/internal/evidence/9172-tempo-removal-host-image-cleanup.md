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
copies `/go-bin/` wholesale, so no other binary is affected. The chart
`tempo-collector` Deployment and both Compose `collector-tempo`
services now run the satellite image
`ghcr.io/eshu-hq/eshu-collector-tempo:v0.1.3` (the QA-proven satellite
from the lane commit message) with the identical binary path
`/usr/local/bin/eshu-collector-tempo`, identical command flags, and
the identical `ESHU_COLLECTOR_INSTANCES_JSON` tempo instance shape
(`collector_kind: tempo`, `mode: continuous`, claim-driven). Service,
Deployment, and profile names are unchanged, and the workload stays
default-disabled in chart and Compose.

Measured at this HEAD:

- `go test ./internal/runtime/ -count=1` passes, including the Helm
  observability-collectors contract (renders the tempo Deployment with
  the satellite image and asserts command plus claim env) and the
  Compose preflight/render tests.
- `bash scripts/test-remote-e2e-hosted-compose-render.sh` passes
  (tempo service present under the tempo profile with the satellite
  image and unchanged env contract).
- `bash scripts/verify-compose-helm-runtime-parity.sh` passes
  (observability collectors, probes, and ServiceMonitor coverage).
- `go test ./cmd/capability-inventory/ ./cmd/fact-kind-registry/
  -count=1` passes; `bash scripts/verify-fact-kind-registry.sh`
  reports current; surface inventory reconciles 643 surfaces with no
  findings.
- `helm template` with `tempoCollector.enabled=true` renders
  `image: "ghcr.io/eshu-hq/eshu-collector-tempo:v0.1.3"`.
- `docker compose config` for both Compose files renders
  `collector-tempo` with that image and no `build:` block.

Coordinator claim planning (`planner/tempo`), reducer projection, and
query surfaces are untouched, so in-tree collection semantics cannot
regress: the only in-tree behavior change is the absence of a binary
nothing else in the image referenced.

## Observability: no new or altered telemetry

- No-Observability-Change: this change adds no metrics, spans, logs,
  or status fields. The only Go
edit reorders a producer kind list to the generator-canonical
alphabetical order; the only test edit removes a stale table entry.
Chart and Compose edits change image references, not probes, ports, or
metric wiring (parity script confirms probes and ServiceMonitor
coverage). Collection telemetry now comes from the satellite binary,
which is the extracted identical code (byte-identical parity proven by
the #8867 extraction-prep mirror tests); operator-visible signals
(`/healthz`, `/readyz`, `/metrics`, `/admin/status`) keep the same
paths and ports.
