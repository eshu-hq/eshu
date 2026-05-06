# AGENTS.md — internal/iacreachability guidance for LLM assistants

## Read first

1. `go/internal/iacreachability/README.md` — pipeline position, family list,
   lifecycle, and invariants
2. `go/internal/iacreachability/analyzer.go` — `Analyze`, `discoverArtifacts`,
   `buildReferenceIndex`; understand the discovery / reference split before
   adding a family
3. `go/internal/iacreachability/compose.go` — Compose service discovery and
   invocation scanning; a concrete example of the two-function pattern
4. `go/internal/iacreachability/doc.go` — package contract paragraph

## Invariants this package enforces

- **Static analysis only** — the package never executes templates, runs
  Terraform, or calls external tools. All classification derives from file
  content passed in as `File.Content` strings.
- **Determinism** — `Analyze` must produce identical output for identical
  input. Do not use random ordering, time-based logic, or maps without
  explicit sort before output.
- **Ambiguity is a first-class state** — a reference containing `{{` or `${`
  must be recorded as ambiguous, not used and not skipped. Do not silently
  drop template references; they are operator-visible signals.
- **Fixed confidence values** — 0.99 (`in_use`), 0.75 (`candidate_dead_iac`),
  0.40 (`ambiguous_dynamic_reference`). These are part of the cleanup-truth
  product contract. Do not change them without updating the HTTP API docs and
  any downstream sort logic.
- **Output sorted by Row.ID** — `Analyze` and `CleanupRows` both sort by `ID`.
  New code paths that construct rows must not skip the sort.
- **No internal Eshu imports** — `iacreachability` is a pure analysis package.
  Do not import `internal/facts`, `internal/relationships`, or any other Eshu
  package. The only external dependency is `gopkg.in/yaml.v3` for Compose YAML.

## Common changes and how to scope them

- **Add a new IaC artifact family** →
  1. Add a discovery function returning `(path, name, bool)` following
     `terraformModuleArtifact` pattern.
  2. Add a reference recorder following `recordTerraformReferences`, handling
     both static and template-containing references.
  3. Wire both into `discoverArtifacts` and `buildReferenceIndex` in
     `analyzer.go`, guarded by `familyEnabled`.
  4. Extend `RelevantFile` to include the new extensions.
  5. Add tests covering the discovery path, a used artifact, an unused
     artifact, and a template-ambiguous reference.
  Run `go test ./internal/iacreachability -count=1`.

- **Change family filtering** → `Options.Families` is a map from lowercase
  family name to bool. Pass a non-nil map to restrict families; nil means all
  families enabled. Do not add a new boolean field to `Options` for each
  family; extend the map.

- **Add a new Compose detection heuristic** → `compose.go` owns Compose logic.
  Follow `composeServicesFromCommand` and `recordComposeReferences`. Keep the
  command whitelist in `composeServicesFromCommand` conservative; broad
  invocation matching produces false positives.

## Failure modes and how to debug

- Symptom: known-used artifact classified as `candidate_dead_iac` →
  likely cause: reference pattern does not match the actual file content →
  add the raw file content to a test case and trace through the relevant
  `record*References` function; check whether the path segment pattern
  (`/modules/`, `charts/`, `roles/`) matches the actual path shape.

- Symptom: Ansible role never classified as `in_use` →
  likely cause: the playbook that references the role is not reached by an
  `ansible-playbook` invocation → `recordAnsibleReferences` gates role
  recording on `reachedPlaybooks`; check that a `ansible-playbook <playbook>`
  invocation exists in the content passed to `Analyze`.

- Symptom: Compose service always `candidate_dead_iac` →
  likely cause: invocation line does not have a recognized subcommand before
  the service name → inspect `composeServicesFromCommand`; the function skips
  flags and known binary tokens but requires a recognized subcommand word
  before recording service names.

- Symptom: template reference produces `in_use` instead of `ambiguous` →
  likely cause: the reference recorder is missing the `{{` / `${` check →
  check the `strings.Contains(... "{{")` guard in the relevant
  `record*References` function.

## Anti-patterns specific to this package

- **Executing templates or shelling out** — this package must be pure Go with
  static string analysis only. No `os/exec`, no Terraform CLI, no Helm
  binary calls.
- **Importing ESHU-internal packages** — imports of `internal/facts`,
  `internal/relationships`, or any storage package are boundary violations.
- **Silently dropping ambiguous references** — do not filter out template
  expressions. They must enter the ambiguous index so operators see them.
- **Producing rows without sorting** — callers depend on deterministic row
  order for diff stability. Every new code path that returns rows must go
  through the sort at the end of `Analyze` or `CleanupRows`.

## What NOT to change without an ADR

- Confidence values (`0.99`, `0.75`, `0.40`) — these are exposed in the
  cleanup-truth HTTP response and relied on by operator tooling for rank ordering;
  see `docs/docs/reference/http-api.md`.
- `FindingCandidateDead` string value — stored in HTTP responses and
  potentially in operator dashboards; renaming it is a breaking API change.
- `RelevantFile` extension list — removing an extension causes the HTTP handler
  to stop passing those files to `Analyze`, making all artifacts of that type
  appear unused.
