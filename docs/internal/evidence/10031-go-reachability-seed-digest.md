# #10031 go-reachability seed digest pin: no-regression note

Refs #10031, part of eshu-hq/eshu#10002.

## What changed

`go/internal/collector/scannercomponent/manifest.go` pins the
`artifactDigests[IDGoReachability]` digest for the v0.1.0 scanner-worker
release (multi-arch index `sha256:8e4059a2…f5e39284`), plus the admission
test update that mints the digest-pinned artifact. This is a constant-only
change: no query, projection, queue, lease, schema, or concurrency path is
touched, and the component binary is unchanged (the seed image it names
already shipped).

- No-Regression Evidence: the pin changes no code path, only the admitted
  digest value. Baseline is the pre-pin fail-closed refusal (`no published
  image digest pinned`); after the pin, admission passes and the claim path
  runs unchanged. Proof: focused suite
  `go test ./internal/collector/scannercomponent/ -run
  'TestGoReachability|TestManifestFor|TestRequiredGrants'` green, plus the
  live fresh-DB proof run (derive→claim→worker→facts green with exact fact
  counts 2/2/1 on the pinned digest, Neo4j backend, scanner v0.1.0,
  toolchain go1.26.11, fixture at 0f4c6a42, terminal queue state
  completed with the pre-existing auth_denied history only).
- No-Observability-Change: no new spans, metrics, logs, or status rows; the
  existing component-extension and scanner telemetry covers the claim path
  unchanged.
