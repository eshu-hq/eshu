# Evidence: seed-pin fix moves equality from seed to admission (#8878)

Seed compared `--image-ref` to the `ManifestFor` pin compiled into the
scanner binary. That comparison is unsatisfiable: the pin changes the
image digest, so the binary cannot satisfy equality against its own image
(fixed-point impossibility); QA parity proved it live. Per the arbiter
ruling, equality now lives at admission:

- `component/seed.go`: `Seed` keeps fail-closed SHAPE validation only
  (`ghcr.io/eshu-hq/scanner-<name>@sha256:<64 lowercase hex>` with
  `<name>` matching the binary's own component ID, else nothing is
  written) and stamps the accepted ref into `Artifacts[0].Image` of the
  seeded `manifest.yaml`; every other field is the `ManifestFor`
  document.
- `component/reconcile.go`: `VerifySeededArtifact` admits a
  registry-loaded manifest only when its artifact equals the platform pin
  for `eshu.scanner.*`, and denies fail-closed otherwise. Non-scanner IDs
  pass through. Error text names the component ID and the defect, never an
  image value.
- Enforcement sits at the two seeded-manifest consumers on the
  load-then-claim path, before any claim or execution:
  `selectActivation` in `go/cmd/collector-component-extension/config.go`
  (extension worker loader) and `componentCollectorInstancesFromEnv` in
  `go/internal/coordinator/component_activation_config.go` (activation
  planner). Both wrap the denial with the component ID only.
- `component/manifest.go` records the pin semantics: pins are
  platform-side admission truth; the re-pin PR bumps the publish SHA, the
  pins, and the chart `seed.image` entries together.

No-Regression Evidence: component package suite
(`go test ./internal/collector/scannerworker/component/ -count=1`),
extension worker suite
(`go test ./cmd/collector-component-extension -count=1`), coordinator
suite (`go test ./internal/coordinator/ -count=1`), and the runtime
contract suite pinning the mirrored in-repo chart values
(`go test ./internal/runtime/ -run TestScanner -count=1`) all pass; live-chart
`seed.image` sync rides the re-pin PR process, with admission as the
fail-closed backstop. The
new tests fail on the pre-fix tree for the intended reason (pin-equality
rejection at seed, no admission check). The only added production work is
one shape check per seed invocation (cold init path) and one artifact
comparison per activation load (cold startup path): no Cypher, no worker
claims, no leases, no batching, no hot-path change. No benchmark delta is
claimed because no measured path exists to improve.

No-Observability-Change: no metric, span, log, or status field is added,
renamed, or removed. Seed and admission failures return defect-only error
strings to the init-container or startup caller; the stage stays covered
by the `No-Observability-Change` marker in the scanner worker stdio
components row of `docs/public/observability/telemetry-coverage.md`.
