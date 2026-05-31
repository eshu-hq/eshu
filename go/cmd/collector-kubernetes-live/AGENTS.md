# AGENTS.md - cmd/collector-kubernetes-live guidance for LLM assistants

## Read First

1. `go/cmd/collector-kubernetes-live/README.md` - binary purpose, config,
   telemetry, and invariants
2. `go/cmd/collector-kubernetes-live/config.go` - cluster JSON and auth mapping
3. `go/cmd/collector-kubernetes-live/service.go` - client factory and
   `collector.Service` wiring
4. `go/internal/collector/kuberneteslive/` - source, fact, and envelope contracts
5. `go/internal/collector/kuberneteslive/clientgo/` - read-only auth and listing
6. `docs/internal/design/388-kubernetes-live-collector.md` - design and scope

## Invariants This Package Enforces

- READ-ONLY and METADATA-ONLY. The binary lists; it never mutates a cluster and
  never reads Secret values, ConfigMap data, env values, or logs.
- Cluster configs may reference kubeconfig paths, but credentials must not be
  logged, placed in metrics, or written into facts.
- `cluster_id` is operator-declared and durable; never infer identity from the
  API server URL.
- Facts must flow through `collector.Service` and `postgres.NewIngestionStore`.
- Keep this binary registered in `scripts/install-local-binaries.sh`.

## Common Changes And How To Scope Them

- Add a config field with a `config_test.go` case for both happy and error
  paths.
- Add claim-driven mode (deferred) by mirroring the OCI registry binary's
  `claimAwareModeEnabled` dual-build and wiring `collector.ClaimedService`.

## Anti-Patterns

- Logging or emitting kubeconfig contents, tokens, or any credential material.
- Adding a mutating Kubernetes call anywhere in the binary or its packages.
- Bypassing `collector.Service` to write facts or graph state directly.
