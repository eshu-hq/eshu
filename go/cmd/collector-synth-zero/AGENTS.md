# AGENTS.md - collector-synth-zero guidance

Read these before editing this package:

1. `README.md`
2. `doc.go`
3. `go/cmd/collector-component-extension/README.md`
4. `go/internal/collector/extensionhost/README.md`
5. `go/internal/component/README.md`

## Invariants

- Every invocation returns unchanged with status rows only. Never add fact
  emission, source observation, network calls, or credentials.
- Echo the host claim verbatim. Any normalization breaks host identity
  validation and terminal-fails the claim.
- Keep `synth_zero_fact` out of `scope`, fleet lists, and workflow
  contracts. The kind flows as manifest data only.
- The emitted family in `manifest.yaml` stays declared-but-never-emitted.
- `deployment.yaml`, `networkpolicy.yaml`, and the seed image are authored,
  not applied. Never point them at a live cluster from this lane.
- The producer pin in `deployment.yaml` must equal `manifest.yaml`
  metadata id and version. The pin test enforces this.

## Common changes

- Emitter behavior changes need a failing test first, then the pin suite,
  then the live end-to-end. Mutate the unchanged path to prove sensitivity.
- Manifest, runtime, trust, egress, and Deployment edits must keep every
  `TestWindow*` test green; they cross-check the files against each other.
- Rebuilding the seed image re-pins the digest in `manifest.yaml` and
  `deployment.yaml` in the same commit.

## Failure modes

- Undecodable stdin fails closed with no stdout bytes.
- Oversize stdin fails closed at the 4 MiB cap.
- A dropped emission family fails host admission, not the window.
- A drifted producer pin fails worker startup (fail closed, by design).
