# AGENTS.md - internal/parser/dockerfile guidance

## Read first

1. README.md - package boundary, metadata fields, and invariants
2. doc.go - godoc contract for the Dockerfile helper package
3. metadata.go - Dockerfile instruction parsing and payload compatibility map
4. base_ref.go and base_ref_expand.go - ARG substitution into FROM image
   references (global-scope rule and evidence capping in base_ref.go; the
   expander, supported operators, and unresolved markers in base_ref_expand.go)
5. tokens.go - Dockerfile escape directive detection and command-line token
   splitting for quoted and escaped metadata values
6. metadata_test.go, base_ref_test.go, base_ref_bound_test.go,
   base_ref_cap_test.go, base_ref_nested_cost_test.go and
   base_ref_payload_test.go - behavior, expansion-bound, allocation-bound and
   evidence-payload coverage for runtime metadata and map shape

## Invariants this package enforces

- Dependency direction stays one way: parent parser code may import this
  package, but this package must not import internal/parser.
- RuntimeMetadata returns typed evidence; Metadata.Map is the compatibility
  bridge for existing payload consumers.
- A FROM line sees only ARGs declared before the first FROM. Never widen that
  scope, and never fill an unresolved value with a guess: keep the placeholder
  and report it in UnresolvedArgs.
- An expansion that produces more than 4096 bytes is unresolved. The cap is
  checked before each write, so the output never passes 4096 bytes. An ARG
  default past the cap is never stored as known.
- Every nesting level of one reference writes into the single expander output
  buffer, and the length is checked before each write. Do not give a nested
  word its own builder: copying results outward costs O(depth^2 x 4096) bytes
  inside the length caps (TestBaseRefNestedPaddedReferenceAllocationIsBounded).
- UnresolvedArgs holds variable names, plus one of the four fixed markers in
  base_ref_expand.go for a construct with no name. Never copy Dockerfile text
  into it.
- The ARG default evidence on a stage (ArgDefaults) is capped at 256 bytes per
  value and 16 entries per stage (names are not capped on their own; they come
  from one reference of at most 4096 bytes, so a stage stays under about 8 KB), with ArgDefaultsTruncated and
  ArgDefaultsOmitted making the cut visible. Resolution reads the declared
  default, never the stored evidence. Do not raise either cap without
  re-measuring the marshaled payload (base_ref_payload_test.go).
- Row ordering must stay deterministic because parser payloads are fact inputs.

## Common changes and how to scope them

- Add Dockerfile evidence by writing a focused test in metadata_test.go first.
- Keep file reading and registry dispatch in the parent parser package.
- Keep query-specific runtime story generation out of this package.

## Failure modes and how to debug

- Missing runtime rows usually mean instruction continuation or key/value token
  parsing changed.
- Query regressions usually mean Metadata.Map drifted from the legacy payload
  shape expected by parser.ExtractDockerfileRuntimeMetadata consumers.

## Anti-patterns specific to this package

- Importing the parent parser package to reuse payload helpers.
- Returning only map[string]any and losing the typed helper contract.
- Adding repository-specific Dockerfile conventions without fixture evidence.

## What NOT to change without an ADR

- Do not move query or relationship interpretation into this package. It owns
  parser evidence only.
