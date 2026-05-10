# AGENTS.md - internal/collector/tfstateruntime guidance for LLM assistants

## Read first

1. `go/internal/collector/tfstateruntime/README.md` - package purpose, flow, and
   invariants
2. `go/internal/collector/tfstateruntime/source.go` - claimed source and default
   source factory
3. `go/internal/collector/terraformstate/README.md` - reader/parser safety rules
4. `go/internal/collector/claimed_service.go` - claim lifecycle, heartbeats, and
   fencing behavior

## Invariants this package enforces

- Do not persist, log, or put raw Terraform state bytes in errors, facts, spans,
  metrics, or docs.
- Only exact candidates from `terraformstate.DiscoveryResolver` may be opened.
- Current local state reads must come from explicit absolute operator sources.
  Repo-local candidate approval is tracked by #140 and must not be treated as
  available until that path lands.
- Keep AWS SDK types out of this package. Use `terraformstate.S3ObjectClient`
  and put SDK adapters in command or integration wiring.
- A claimed item must match scope ID, generation ID, and source run ID before a
  collected generation is returned.
- Fencing tokens must come from the current workflow claim.

## Common changes and how to scope them

- Add a new backend by extending `DefaultSourceFactory` after the reader package
  exposes a safe exact-source type.
- Add telemetry around runtime integration here, not in the redaction package.
- Add cloud-provider SDK behavior in a small adapter that implements the reader
  interface. Do not leak SDK models into parser code.

## Anti-patterns

- Reading an entire state payload into memory just to derive serial and lineage.
- Returning a generation for a claimed item that does not match the derived
  state snapshot identity.
- Opening prefix-based S3 keys, workspace directories, guessed local files, or
  repo-local state candidates before the #140 approval path exists.
- Adding storage, graph, reducer, or query dependencies to this package.
