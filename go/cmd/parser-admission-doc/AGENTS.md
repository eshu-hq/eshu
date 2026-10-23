# AGENTS.md - cmd/parser-admission-doc guidance

## Read first

1. `README.md` - purpose and flags.
2. `go/internal/parser/registry_admission.go` - table renderer and marker
   replacement. Rendering logic lives there, not here.
3. `go/internal/parser/registry_definitions.go` - the data being rendered.

## Invariants

- Build-time only; do not add runtime dependencies.
- Output must stay byte-deterministic. Never write a timestamp or iterate a map.
- Never hand-edit the generated region of
  `docs/public/languages/admitted-files.md`. Change the registry and rerun
  `scripts/generate-parser-admission-doc.sh`.
- `-check` must not write.
