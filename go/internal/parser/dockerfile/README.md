# Dockerfile Parser Helpers

## Purpose

This package owns Dockerfile parser helpers that do not need parent parser
payload helpers. It extracts runtime metadata such as stages, exposed ports,
arguments, environment variables, labels, build platforms, and runtime commands
from Dockerfile source text.

## Ownership boundary

The package is responsible for typed Dockerfile runtime evidence. The parent
parser package still owns file I/O, parser registry dispatch, payload assembly,
and the exported compatibility wrapper used by query and relationship code.

## Exported surface

The godoc contract is in doc.go. Current exports are Metadata, Stage, Arg, Env,
Port, Label, RuntimeMetadata, and Metadata.Map. The Stage type carries
BaseRefTemplate, UnresolvedArgs, and ArgDefaults for the FROM substitution
evidence above.

## Dependencies

This package imports only the Go standard library. It must not import the
parent parser package, collector packages, graph storage, or reducer code.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

RuntimeMetadata keeps bucket ordering deterministic by sorting rows by name,
matching the old parent parser payload behavior.

RuntimeMetadata follows Dockerfile command-line token rules for the metadata it
models: quoted and escaped `ENV` and `LABEL` values keep spaces, legacy
`ENV key value` lines emit a single environment row, multi-argument `ARG`
instructions emit one row per argument, `FROM --platform=...` records stage
platform metadata, registry hosts with ports do not get split as image tags,
and the Dockerfile `# escape=` parser directive controls line continuation plus
token escaping when it appears before any non-directive comment, empty line, or
instruction. The tokenization helpers live in `tokens.go` so `metadata.go`
stays below the package line cap.

### FROM base-image ARG substitution

`base_ref.go` expands ARG references in a `FROM` image reference, following the
Dockerfile rule for how `ARG` and `FROM` interact:

- Only ARGs declared before the first `FROM` are visible to a `FROM` line. An
  ARG declared inside a stage, including after the stage's own `FROM`, is never
  visible to that stage's `FROM` or to any later one.
- `$VAR`, `${VAR}`, `${VAR:-word}`, `${VAR-word}`, `${VAR:+word}`, and
  `${VAR+word}` are expanded. A global ARG default may itself reference an
  earlier global ARG. The last global declaration of a name wins. The
  expansion runs before the reference is split into image and tag, so
  `ARG BASE=golang:1.24` with `FROM $BASE` yields image `golang`, tag `1.24`.
  Digest pins, `--platform`, `AS`, `FROM scratch`, and `FROM <earlier stage>`
  are unchanged. The `--platform` value is kept as written.
- A reference stays unresolved when any variable has no value known at parse
  time: an ARG without a default, an undeclared variable used without an
  operator, an ARG whose default is itself unresolved, a predefined platform
  ARG such as `TARGETARCH` (its default is only a default), or an expression
  such as `${V%%.*}`, `${V:?msg}`, or `$(cmd)`. An ARG declared without a
  default is treated as a possible build argument, so `${V:-lts}` stays
  unresolved when `ARG V` is declared and resolves to `lts` only when `V` is
  never declared.
- When resolved, `base_image` and `base_tag` hold the expanded value, and
  `base_ref_template` holds the reference as written. When unresolved,
  `base_image` and `base_tag` keep the reference as written (placeholder
  included), `base_ref_unresolved_args` lists the variable names, and
  `base_ref_arg_defaults` holds any default seen for them as evidence only
  (for example a stage-local ARG default the `FROM` could not see). All three
  keys are omitted for a `FROM` with no variable, so existing payloads are
  byte-identical. They ride in the open attributes of the typed stage contract.
- An unaliased stage is named after its expanded image.
- Expansion is bounded to 4096 bytes. A Dockerfile is untrusted repository
  content, and ARGs that each repeat the previous one would otherwise grow
  exponentially. A value past the bound is treated as not knowable, so the
  reference stays unresolved.

Known limit: the tokenizer drops backslash escapes and quotes before
expansion, so an escaped `\$` or single-quoted `$` is treated as a variable.

Metadata.Map keeps the legacy `map[string]any` payload shape because query and
relationship packages still consume `parser.ExtractDockerfileRuntimeMetadata`.

## Related docs

- docs/public/languages/support-maturity.md
