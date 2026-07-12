<!-- docs-catalog
title: Ifá CLI Reference
description: Documents the seven ifa subcommands, their flags, and one realistic invocation each.
type: reference
audience: practitioner
entrypoint: false
landing: false
-->

# Ifá CLI reference

`ifa` (`go/cmd/ifa`) is the Ifá conformance platform's command-line entry
point. It dispatches to seven subcommands; running it with no arguments or an
unknown subcommand prints usage. Flags below are taken directly from each
subcommand's `-h` output.

## `coverage`

Reconciles derived Ifá coverage against `specs/ifa-coverage-manifest.v1.yaml`.
Advisory by default; `-blocking` is the mode `make prove` and CI use.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-blocking` | off | Fail on any uncovered, unresolved, or stale surface. |
| `-gates` | `<specs-dir>/ci-gates.v1.yaml` | Path to the CI gate registry. |
| `-manifest` | `<specs-dir>/ifa-coverage-manifest.v1.yaml` | Path to the Ifá coverage manifest. |
| `-replay-manifest` | `<specs-dir>/replay-coverage-manifest.v1.yaml` | Path to the replay coverage manifest. |
| `-repo-root` | `.` | Repository root. Unused today; kept for parity with `replay-coverage-gate`. |
| `-report-out` | none | Path to write the JSON coverage report. |
| `-snapshot` | `testdata/golden/e2e-20repo-snapshot.json` | Path to the B-12 golden snapshot. |
| `-specs-dir` | `specs` | Directory holding the registry specs. |

```bash
cd go
go run ./cmd/ifa coverage \
  -specs-dir ../specs \
  -snapshot ../testdata/golden/e2e-20repo-snapshot.json
```

## `expectations`

Prints the derived expectation for one or every fact kind, straight from the
fact-kind registry, the B-12 snapshot, and the replay-coverage manifest — the
same join `coverage` uses internally.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-format` | `json` | Output format (only `json` is supported). |
| `-kind` | all kinds | Print only the derived expectation for one fact kind. |
| `-replay-manifest` | `<specs-dir>/replay-coverage-manifest.v1.yaml` | Path to the replay coverage manifest. |
| `-snapshot` | `testdata/golden/e2e-20repo-snapshot.json` | Path to the B-12 golden snapshot. |
| `-specs-dir` | `specs` | Directory holding the registry specs. |

```bash
cd go
go run ./cmd/ifa expectations \
  -specs-dir ../specs \
  -snapshot ../testdata/golden/e2e-20repo-snapshot.json \
  -kind gcp_cloud_resource
```

Prints the kind's read surface, its query-truth binding, and its payload
schema path — for example, `gcp_cloud_resource` derives to
`GET /api/v0/cloud/inventory` and
`sdk/go/factschema/schema/gcp_cloud_resource.v1.schema.json`.

## `drive`

Replays a cassette through the concurrent driver straight into a Postgres
`IngestionStore` — the same durable commit boundary a live collector uses,
without compiling or invoking any collector binary.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-cassette` | required | Path to a replay/cassette JSON file. |
| `-postgres-dsn` | `ESHU_POSTGRES_DSN`/`ESHU_FACT_STORE_DSN`/`ESHU_CONTENT_STORE_DSN` | Postgres DSN to commit into. |
| `-workers` | `1` | Number of concurrent Driver workers draining the cassette. |

```bash
cd go
go run ./cmd/ifa drive \
  -cassette ../testdata/cassettes/gcpcloud/supply-chain-demo.json \
  -workers 4
```

## `graph-dump`

Reads the live graph backend over `NEO4J_*`/`ESHU_GRAPH_BACKEND` and prints
its content-addressed canonical form — the comparator the determinism matrix
uses to prove two runs produced the identical graph.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-digest` | off | Print the sha256 digest of the canonical graph instead of its full canonical bytes. |
| `-out` | stdout | Path to write the canonical graph dump. |

```bash
cd go
go run ./cmd/ifa graph-dump -digest
```

## `mutate-cassette`

Deterministically corrupts facts in a cassette copy — the failure-path
fixture generator for the dead-letter determinism matrix. Never mutates the
source cassette; always writes a clone. See
[Odù and cassettes](../concepts/ifa-conformance-platform.md#odu-and-cassettes)
for what each mutation kind reaches at runtime.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-cassette` | required | Path to the source replay/cassette JSON file. |
| `-count` | `1` | Number of facts to mutate, selected deterministically by ascending `stable_fact_key`. |
| `-fact-kind` | required | Fact kind eligible for mutation, e.g. `gcp_cloud_resource`. |
| `-field` | — | Required payload field to delete (required for `-kind=missing-field`). |
| `-kind` | required | Mutation kind: `missing-field` or `schema-major`. |
| `-out` | required | Path to write the mutated cassette JSON file. |
| `-schema-major` | — | Replacement `schema_version`, e.g. `99.0.0` (required for `-kind=schema-major`). |

```bash
cd go
go run ./cmd/ifa mutate-cassette \
  -cassette ../testdata/cassettes/gcpcloud/supply-chain-demo.json \
  -fact-kind gcp_cloud_resource \
  -kind schema-major \
  -schema-major 99.0.0 \
  -out /tmp/mutated-schema-major.json
```

## `dead-letters`

Reads the durable `fact_work_items` dead-letter set from Postgres and prints
it as JSON — the same shape `DeadLetterSetsEqual` compares across worker
counts in the determinism matrix.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-out` | stdout | Path to write the dead-letter set JSON. |
| `-postgres-dsn` | `ESHU_POSTGRES_DSN`/`ESHU_FACT_STORE_DSN`/`ESHU_CONTENT_STORE_DSN` | Postgres DSN to read from. |

```bash
cd go
ESHU_POSTGRES_DSN=postgresql://eshu:change-me@localhost:15432/eshu \
  go run ./cmd/ifa dead-letters
```

## `synth-cassette`

Generates a deterministic, seeded synthetic GCP cassette — the public,
shareable corpus path
[Odù and cassettes](../concepts/ifa-conformance-platform.md#odu-and-cassettes)
describes. Same seed, same bytes, every time.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-divergent` | off | With `-overlap`, mutate each scope's observed state so the shared-uid scopes carry divergent payloads. |
| `-out` | required | Path to write the generated cassette JSON file. |
| `-overlap` | off | Generate the #5007 contention fixture (K scopes sharing one resource-identity set) instead of disjoint scopes. |
| `-projects` | `4` | Number K of GCP project scopes to generate. |
| `-resources` | `16` | Number of `gcp_cloud_resource` facts to generate per scope. |
| `-seed` | required | Deterministic PRNG seed. |

```bash
cd go
go run ./cmd/ifa synth-cassette \
  -seed 4396 \
  -projects 8 \
  -resources 64 \
  -out /tmp/synth-multiscope.json
```
