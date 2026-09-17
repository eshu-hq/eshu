# Collector Repository Template

Starter for a standalone `eshu-collector-<source>` repository. Copy this
directory as the new repository root, then rename once: module path,
`ComponentID`, `CollectorKind`, `SourceSystem`, manifest `metadata.id`, and
`metricsPrefix`. This template pins the published SDK pair from the
sdk-compatibility row for core `>=0.0.5 <0.2.0`:

- `github.com/eshu-hq/eshu/sdk/go/collector v0.2.0`
- `github.com/eshu-hq/eshu/sdk/go/factschema v0.2.0`

Wire protocol: `collector-sdk/v1alpha1`. There are no `replace` directives:
an out-of-tree clone resolves both modules through the Go module proxy.

## Naming

The directory is `collector` and the binary is `cmd/collector` — the
repository name already says which collector this is, so file, package, and
symbol names do not repeat it (`collector/collector-foo` is rejected in
review). Keep internal helpers private.

## Build and test from a clean clone

```bash
go build ./...
go vet ./...
go test ./... -count=1
go run ./cmd/collector --input ./testdata/complete.json
```

`scripts/run-conformance.sh` runs the same gate CI runs
(`.github/workflows/ci.yml`, discovered by GitHub Actions when this directory
becomes a repository root).

## Configuration

Copy `config.example.yaml`. The collector reads one source document
(`source.input`), emits against one credential-free `sourceURI`, and tracks
freshness with `freshness.previousDigest`. All payloads pass
`ValidateShareSafeKeys`; anything sensitive must be redacted before emission
(see `collector.go` and the redaction test).

## Least-privilege credentials

The collector receives only a bounded claim plus local config. It never gets
Postgres, graph, queue, lease, reducer, API, or MCP handles. Source
credentials stay in the collector runtime environment and never enter fact
payloads, source URIs, logs, or statuses. URIs embedding `user:secret@host`
or `password=`/`secret=`/`token=` query shapes are refused.

## Limits

Default bounds live in `health.go` (`DefaultResourceUse`): 5000 records per
claim, 64 KiB per payload, 300 s claim timeout, 8 queued retries. Tune them
per source and record the values in the cutover issue. Past the retry bound
the collector fails terminal instead of queuing forever.

## Upgrades

Pin exact SDK versions. Before bumping either pin, re-run conformance in the
collector's own CI: a payload shape the new schemas reject fails closed here
before it ever reaches a reducer. Keep the compatibility row in step with
`docs/public/extend/sdk-compatibility.md`.

## Producer grants, revocation, and rollback

Core-owned (unprefixed) fact kinds require a live #6709 producer grant naming
this producer, version, kind, schema, and scope; namespaced kinds need none.
`grant.example.json` is the issuance request shape; `grant_test.go` locks the
fail-closed matrix (wrong scope/schema/producer refused). Revocation is a
core-side grant revoke followed by replay from the last committed generation:
stop new claims, expire in-flight leases, revoke, restore the last supported
producer, replay, and verify fact/graph/read truth. Two producers never hold
one scope+generation at once.

## Releases

Every artifact in `manifest.yaml` is digest-pinned (`@sha256:…`, never
`:latest`). `manifest_test.go` enforces pinning plus manifest validity, and
the all-zero placeholder digest is rejected as soon as you rename the
component — so after renaming, CI stays red until you resolve the real pushed
digest with `scripts/pin-digest.sh`. Base images in `Dockerfile` are pinned
the same way; re-pin them when bumping either base.

## Remote proof

`compose/docker-compose.yml` plus `scripts/run-remote-readback.sh` submit
one claim to a target core and assert every emitted stable key through graph,
API, and MCP readback. All four surface bindings are required with no
defaults — an unconfigured surface fails the script instead of reporting
unmeasured success — and the endpoint paths come from the target core's own
API reference, never invented here. Record the tested core version, topology,
and evidence location in the cutover issue; credential-free conformance stays
in CI.
