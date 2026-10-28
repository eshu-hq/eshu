# Remote Claim Client Agent Rules

Prototype seam for the extension-host extraction (`eshu-hq/eshu#8877`).
Follow the root `AGENTS.md`, `docs/internal/agent-guide.md`,
`go/internal/collector/AGENTS.md`, and the parent
`go/internal/collector/extensionhost/AGENTS.md`.

- Do not wire this package into any in-tree binary, runner, or service.
  `collector.ClaimedService` stays the only workflow claim mutation
  owner until the cutover harness lands. This package has no production
  callers by design.
- Do not invent protocol: cover exactly what the merged `workflowv1`
  table declares. A missing response shape (pacing consult body,
  empty-queue signal, grant plane) is a documented cutover gap, never a
  reason for an interim one-off endpoint.
- Keep the holder identity construction-time. Never accept producer id,
  version, or scope from a response, argument, or config read at emission
  time; `AuthorizeEmission` takes kind and schema version only.
- Reuse `component.EvaluateEmission` and `component.ClassifyEmission`
  for every authority check. Do not reimplement grant predicates here.
- Keep errors identifier-only and bounded. Do not include response
  bodies, payloads, credentials, file paths, or high-cardinality values
  in errors, decisions, or observer records. The bearer travels as a
  header only and is scrubbed from surfaced text.
- Report grant decisions at the owning site (`VerifyWiring` for wiring,
  `AuthorizeEmission` for emission), once per evaluated core-owned
  kind. Observation must never change the allow/deny outcome.
- Add failing tests first for every behavior change, replaying the
  corresponding `EXTRACTION_PREP.md` §7 row through the client. Keep the
  row-to-test mapping table at the top of `replay_test.go` current.
- Keep every file under 500 lines. Run `gofumpt`, `go vet`, the package
  tests with `-race`, `scripts/verify-package-docs.sh`, and a secret
  scan before committing.
