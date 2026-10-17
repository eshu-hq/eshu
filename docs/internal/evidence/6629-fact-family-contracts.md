# #6629 code-first fact-family contracts — performance and observability evidence

No-Regression Evidence: the change adds build-time contract declarations
only. In reducer packages it adds unused exported package-level variables
(`contract.Bind` results in `fact_bindings.go` files); `Declare`/`Bind` are
identity functions in `go/internal/facts/contract`, so no reducer, projector,
queue, Cypher, or admission code path changes shape, takes a new lock, or
gains a new branch. Runtime truth is pinned three ways on this branch:
(1) the generated `fact_kind_registry.generated.go` and
`FACT_KIND_REGISTRIES.md` rows are byte-identical to the pre-change
YAML-derived baseline (only the one-line provenance headers differ), and the
generated `specs/fact-kind-registry.v1.yaml` is semantically identical under
parsed-YAML comparison, so schema-version classifications and admission
behavior cannot drift; (2) `go test ./internal/facts/... -count=1`,
`go test ./internal/mcp/ -run TestEveryRegistryKindHasConsumerOrDisclosure
-count=1`, the contracttest, factschema-diff, and payload-usage gates, and
sampled binding-package tests (obscoverage, servicecatalog,
supplychain/core, code/owners) all pass; (3) the B-7/B-12 static mirror
(`scripts/test-verify-golden-corpus-gate.sh`) passes and
`testdata/golden/e2e-20repo-snapshot.json` is byte-identical (empty diff).
The only executed cost that changes is the build-time generator itself:
baseline `bash scripts/verify-fact-kind-registry.sh` at 5e6fa5c5fb29 took
6.07s wall (4.43s user); on this branch the same command, now also scanning
the Go tree with `go/parser` and verifying 310 generated files, took 6.92s
wall (3.11s user) on the same host with `GOTOOLCHAIN=go1.26.9` — inside the
compile-cache noise of the `go run` wrapper, on a developer/CI gate path
only, with no queue, row-count, or backend involvement. Input shape: 30
families, 182 kinds, 65 reducer bindings, full `go/` tree parse; no
Postgres, Neo4j, or NornicDB backend is touched by the generator.

No-Observability-Change: no runtime component changes behavior, so no new
spans, metrics, or log keys are introduced and none are removed. The
generator remains a build-time CLI whose operator signal is its existing
stderr/exit-code contract, now with file:line + corrective-action
diagnostics for declaration failures. Verification: `go vet
./internal/facts/... ./cmd/fact-kind-registry/` clean, telemetry-selected
gates in `run-selected-gates.sh` pass, and the diff contains no changes under
any telemetry registration surface.
