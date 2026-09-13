# #6627 CI/CD run-correlation projector package move

Date: 2026-09-12

Base commit: `bcc7aa1a0aa903d73b5eda72e6de87e302769902`

## Scope

This slice moves the pure CI/CD run-correlation intent builder from
`internal/projector/cicdruncorrelation` to
`internal/projector/cicd/run/correlation`. It adds documentation-only `cicd`
and `cicd/run` parents and renames the sole export to `BuildReducerIntent`.

Root projector assembly still owns the immutable fact lookup, ordered fan-out,
queue writes, retries, and telemetry. The reducer still owns correlation,
cross-scope reads, and durable writes. There is no Cypher, storage, query,
backend configuration, or deployment change.

## TDD and equivalence

The moved test was changed to package `correlation` and to call
`BuildReducerIntent` before the implementation moved. The red run failed with
undefined `BuildReducerIntent` errors in all seven call sites:

```text
go test ./internal/projector/cicd/run/correlation -count=1
exit 1
```

After moving and renaming the builder, the direct leaf and root dispatcher
contracts passed:

```text
go test ./internal/projector/cicd/run/correlation ./internal/projector \
  -run 'TestBuildReducerIntent|TestBuildProjectionQueues(SingleCICDRunCorrelationIntentForRunFact|CICDRunCorrelationIntentForArtifactOnlyGeneration|CICDRunCorrelationIntentForRunAndArtifactGeneration|NoCICDRunCorrelationIntentWithoutCICDFacts)|TestReducerIntentProbeCountMatchesDocumentedCount|TestAppendScopeGenerationReducerIntentsFanOutParity' \
  -count=1
exit 0
```

Normalized diffs of the old and new production files and tests both returned
exit 0 after substituting only the package, exported builder, and test names.
The root dispatcher remains at 44 probes. CI/CD correlation is still probe 31,
after container-image identity and before SBOM attachment.

## Local verification

All commands ran with Go 1.26.6, Clang, and `CGO_CFLAGS=-std=gnu17`.

```text
go test ./internal/projector/... -count=1
exit 0

go test -race ./internal/projector/cicd/run/correlation ./internal/projector -count=1
exit 0

go build ./...
exit 0

go vet ./...
exit 0

go doc ./internal/projector/cicd
go doc ./internal/projector/cicd/run
go doc ./internal/projector/cicd/run/correlation
all exit 0

bash scripts/dev/precommit-go.sh dirgate-all
bash scripts/verify-telemetry-coverage.sh
bash scripts/verify-moved-file-refs.sh
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
all exit 0
```

The leaf dependency list does not contain the root
`github.com/eshu-hq/eshu/go/internal/projector` package.

## Replay and golden truth

Static harness tests and the golden gate unit tests passed:

```text
go test ./cmd/golden-corpus-gate -count=1
bash scripts/test-verify-golden-corpus-gate.sh
bash scripts/test-verify-replay-tier.sh
all exit 0
```

`bash scripts/verify-replay-tier.sh` passed against its committed, digest-locked
NornicDB v1.2.3 image. The offline graph-truth lane completed in 123 seconds;
the NornicDB OPTIONAL MATCH checks, full offline replay graph truth, provenance
tombstone truth, and SQL relationship branch non-vacuity all passed.

`bash scripts/verify-golden-corpus-gate.sh` passed with 561 checks, zero
required failures, and one advisory warning. Every observed drain reported zero
residual fact work and zero required nonterminal shared projection intents. The
measured pipeline completed in 199 seconds against a 1,800-second ceiling. The
advisory was `phase_maintenance_drains`: 37 seconds observed against a
30-second ceiling.

The committed cassette and B-12 artifacts did not change:

- `testdata/cassettes/cicdrun/supply-chain-demo.json`: Git blob
  `43d4653012441bfe361b3ce4f223bd433e091623`, SHA-256
  `57bff99713db31ccaf1a958a3feee82b2752bc5f8d9cbc74ff9505b54e8f1c2e`.
- `testdata/cassettes/replayoffline/ci-cd-run-correlation.cost-budget.json`:
  Git blob `ff7153bc517f23d5a2180bc5e95ce28b751580cc`, SHA-256
  `ecf0c09909c161c248db4b669362ac7e0ac69d98e1c2ca08394808c62bf55098`.
- `testdata/golden/e2e-20repo-snapshot.json`: Git blob
  `e28983e5ad44eb23de8751ed97dfc7d6eb99e472`, SHA-256
  `894b8e51905461c8f328a52e5b1c789d89a54c7c24bb14b14785d62ae357fe63`.

## NornicDB version boundary

These runs prove preserved trigger, intent, fan-out, reducer, and query truth
against the repository's committed baselines. They do not prove deployed
NornicDB v1.3.1 compatibility: replay tier pins v1.2.3, while B-7 builds source
commit `3722b483c02c38a8e046d198f8768f200f31023c`. This package move makes no
backend-version claim and leaves image/default/storage alignment and v1.3.1
validation to #6657.

## Operational impact

No-Regression Evidence: the accepted baseline is the byte-identical normalized
production and test source plus the unchanged committed cassette and B-12
contracts. After the move, the digest-locked NornicDB v1.2.3 replay tier passed
in 123 seconds, and the full committed B-7 cassette corpus completed in 199
seconds against its 1,800-second ceiling with 561 checks, zero required
failures, and zero residual fact work or required nonterminal shared projection
intents. The move changes no algorithm, input shape, allocation, I/O, storage
query, queue cardinality, lock, goroutine, or concurrency boundary.

No-Observability-Change: the moved builder emits no signal. Root enqueue and
projector duration metrics remain unchanged; reducer execution, CI/CD
correlation, and Postgres query metrics retain their existing ownership.
