# demospec

## Purpose

`demospec` loads `specs/demo-first-answers.v1.yaml`, the acceptance oracle for
issue #4741 (epic #4592, "first-run experience — zero-credential demo mode").
That spec pins exactly five demo questions to bounded, already-shipped read
surfaces and to the golden-corpus-gate artifacts each answer depends on, so
every sibling issue under the epic (the `eshu demo` command, the guided
CLI/MCP path, the console overlay) tests against one shared, verified
contract instead of inventing its own demo queries.

## Ownership boundary

This package owns loading and validating the manifest's shape (exactly five
questions, non-blank fields, a recognized surface kind, at least one expected
field/path, at least one demonstrated correlation). It does NOT own:

- generating or regenerating the demo corpus (that is `#4581`'s generator
  work and the fixture repos under `tests/fixtures/ecosystems/`),
- running the demo command or the guided query path (`#4743`, `#4745`),
- proving the golden-corpus gate itself is green (`scripts/verify-golden-corpus-gate.sh`,
  `go/cmd/golden-corpus-gate`).

## Exported surface

- `LoadManifest(path string) (Manifest, error)` — reads and validates the
  manifest at `path`.
- `Manifest`, `Question`, `Surface`, `SurfaceKind`, `ExecuteTarget`,
  `ExpectedAnswer`, `Artifacts` — the parsed manifest shape. See `doc.go` for
  the godoc contract.
- `ManifestFileName` — `"demo-first-answers.v1.yaml"`, the file's name under
  `specs/`.

Two fields make each question gate-executable by the demo-answers phase of the
golden-corpus gate (issue #4776): `Surface.Execute` (an `ExecuteTarget` naming
the underlying mcp tool or http route for a playbook surface, whose id is not
directly callable) and `ExpectedAnswer.MinimumResults` (the floor the answer's
first result array must meet, so a demo answer that regresses to empty turns
the gate red). `MinimumResults: 0` asserts field presence only, for
object-shaped answers with no result array.

## Dependencies

- `gopkg.in/yaml.v3` for parsing.
- No internal Eshu package dependency at the loader level. The package's test
  suite (not the loader) imports `go/internal/query` to check playbook IDs
  against `query.PlaybookCatalog()` — a test-only dependency, so `demospec`
  itself stays import-cycle-free relative to `query`.

## Telemetry

None. This is a load-time spec/config loader used by tests, not a runtime
read path; it emits no metrics, spans, or logs.

## Gotchas / invariants

- `specs/` is outside the Go module root (`go/go.mod`), so the manifest
  cannot be `go:embed`-ed. The test suite locates the repository root via
  `runtime.Caller` walking upward until it finds `specs/demo-first-answers.v1.yaml`,
  then reads the file with `os.ReadFile` (`#nosec G304` — the path is
  repo-owned, not external input).
- `LoadManifest` validates shape only. Referential integrity — that every
  `artifacts.cassettes[]` entry resolves to
  `testdata/cassettes/<family>/supply-chain-demo.json`, every
  `artifacts.repos[]` entry resolves to a directory under
  `tests/fixtures/ecosystems/`, every `kind: playbook` ref is a real
  `query.PlaybookCatalog()` ID, and every `kind: mcp/cli/http` ref matches a
  key in `testdata/golden/e2e-20repo-snapshot.json`'s `query_shapes` — is
  proven only by `manifest_test.go`'s `TestDemoFirstAnswers`. Do not assume a
  manifest that parses is a manifest that is correct.
- HTTP surface refs are matched by method + path prefix, ignoring the
  querystring, because the golden snapshot's HTTP keys embed a specific query
  (e.g. `?provider=tempo&limit=50`) that a manifest entry need not repeat
  verbatim.
- The manifest must declare **exactly five** questions. This is an acceptance
  criterion from issue #4741, not an arbitrary cap; `LoadManifest` rejects any
  other count.

## Related docs

- `specs/README.md` — the specs registry, including this manifest's entry.
- `docs/public/reference/local-testing.md` — local gate reference.
