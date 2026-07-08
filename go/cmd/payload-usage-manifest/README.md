# payload-usage-manifest

The `payload-usage-manifest` command is the CLI front end for Contract System
v1 §6 enforcement gate 2 — the payload-usage manifest
([design doc](../../../docs/internal/design/contract-system-v1.md#6-enforcement-gates),
[issue #4573](https://github.com/eshu-hq/eshu/issues/4573)).

All derivation and comparison logic lives in
[`internal/payloadusage`](../../internal/payloadusage); this command is a
thin CLI wrapper.

## Ownership boundary

This command owns only the CLI surface: flag parsing, invoking
`payloadusage.Load`/`payloadusage.Gate`, and writing the result. It does not
own payload struct definitions (`sdk/go/factschema/{aws,iam}/v1/...`), handler
business logic, or the forward-direction schema-diff gate (a
separate, already-landed gate,
[`go/cmd/factschema-diff`](../factschema-diff), issue #4569).

## Modes

### generate

```bash
go run ./cmd/payload-usage-manifest -mode generate [-out <path>]
```

Runs the full derivation and prints the manifest as indented JSON (to stdout
by default, or to `-out <path>`). Useful for inspecting what the gate
currently sees, or for committing a manifest artifact if a future PR chooses
to check one in.

### gate

```bash
go run ./cmd/payload-usage-manifest -mode gate
```

Runs the full derivation, compares every used field against the checked-in
JSON Schema's declared properties
(`sdk/go/factschema/schema/*.json`), and exits non-zero if any handler reads
a field its fact kind's schema does not declare. Each violation names the
specific handler file, fact kind, and field.

Gate mode also enforces the raw-payload convention on the loader,
relationships, and replay surfaces: existing raw reads are explicit exemptions,
and a new `.Payload["field"]` or `payloadString` / `payloadStrings` read fails
unless it is intentionally reviewed into that shrinking list.

## Flags

| Flag | Default | Purpose |
| --- | --- | --- |
| `-repo-root` | `.` | Repository root; every other path defaults relative to it |
| `-reducer-dir` | `<repo-root>/go/internal/reducer` | Reducer handler source directory |
| `-decode-file` | `<reducer-dir>/factschema_decode*.go` glob | Optional single reducer decode-seam file override |
| `-schema-dir` | `<repo-root>/sdk/go/factschema/schema` | Checked-in JSON Schemas (the gate's declared-field source of truth) |
| `-loader-dir` | `<repo-root>/go/internal/storage/postgres` | Loader/persistence source directory |
| `-relationships-dir` | `<repo-root>/go/internal/relationships` | Relationship evidence source directory |
| `-replay-dir` | `<repo-root>/go/internal/replay/offlinetier` | Replay/offline-tier source directory |
| `-aws-struct-dir` | `<repo-root>/sdk/go/factschema/aws/v1` | Typed AWS struct package |
| `-iam-struct-dir` | `<repo-root>/sdk/go/factschema/iam/v1` | Typed IAM struct package |
| `-incident-struct-dir` | `<repo-root>/sdk/go/factschema/incident/v1` | Typed incident struct package |
| `-mode` | `gate` | `generate` or `gate` |
| `-out` | stdout | `generate` mode's output file path |

## Exit status

- `0` — `generate` always succeeds if parsing succeeds; `gate` succeeds if no
  handler reads an undeclared field.
- `1` — a usage/parse error occurred, or (`gate` mode) one or more handlers
  read a field absent from that fact kind's declared schema.

## Wiring

Registered in `specs/ci-gates.v1.yaml` as the `payload-usage-manifest` gate,
triggered by changes under the checked decode/raw-read surfaces and
`sdk/go/factschema/**`. The gate command exercised in CI and pre-PR is:

```bash
cd go && go test ./internal/reducer -run TestPayloadUsageManifest -count=1
```

which calls `payloadusage.Gate` directly from inside the reducer package
(see `go/internal/reducer/payload_usage_manifest_test.go`), so a red result
is investigated from inside the package whose handlers it is checking.

## Dependencies

Standard library plus `github.com/eshu-hq/eshu/go/internal/payloadusage`. No
git, network, Postgres, or graph-backend dependency.

## Telemetry

None. This command runs only in local and CI generation/gate contexts, never
in a deployed Eshu process.
