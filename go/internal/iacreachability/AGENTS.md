# internal/iacreachability Agent Instructions

These rules are mandatory for this package. Root `AGENTS.md` still owns the
repo-wide proof, performance, concurrency, and skill-routing rules.

## Read First

1. `README.md` and `doc.go`.
2. `analyzer.go` before changing discovery, reference indexing, sorting, or
   family filtering.
3. `compose.go` before changing Docker Compose service detection.
4. `go/internal/storage/postgres/README.md` before changing materialized row
   expectations.

## Local Rules

- Keep analysis static. Do not execute templates, Terraform, Helm, Ansible,
  shell commands, or external tools.
- Keep the package pure. It must not import Eshu-internal packages.
- Output must be deterministic for identical input. Sort rows by `Row.ID` for
  returned analysis and cleanup output.
- Ambiguous template references are first-class evidence. Record references
  containing `{{` or `${` as ambiguous; do not silently drop them or promote
  them to used.
- Preserve confidence values unless the API contract changes with proof:
  `0.99` for in-use, `0.75` for candidate-dead IaC, and `0.40` for ambiguous
  dynamic references.
- `Options.Families` is the extension point for family filtering. Do not add a
  boolean field per family.
- `RelevantFile` is a bounded prefilter; removing extensions can make entire
  artifact families disappear from HTTP analysis.

## Change Gates

- New artifact families need discovery, reference recording, `RelevantFile`
  coverage, family filtering, and tests for used, unused, and ambiguous cases.
- Compose heuristics must stay conservative. Broad command matching requires
  false-positive tests.
- API-visible finding or confidence changes require HTTP docs and response
  ordering proof.

## Do Not Change Without Owner Review

- Confidence constants.
- Finding string values.
- Package boundary: no Eshu-internal imports and no runtime execution.
