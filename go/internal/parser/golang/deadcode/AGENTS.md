# AGENTS.md - internal/parser/golang/deadcode guidance

## Read first

1. `README.md` - package boundary, exported surface, and invariants
2. `doc.go` - godoc contract: what root-kind evidence this package emits and why
3. `registrations.go` - explicit net/http and cobra registration matching
4. `roots.go` - `Evidence`/`EvidenceSet`, `RootKinds`, and same-package
   direct-method-call roots
5. `semantic/` - the sibling package this one composes with (interface
   satisfaction, function-value references, generic constraints,
   dependency-injection callbacks)
6. Callers: `internal/parser/golang/language.go` (`Evidence`, `RootKinds`) and
   `internal/parser/golang/framework_routes.go` (`HTTPServeMuxVars`,
   `HTTPHandlerWrapperTarget`)

## Invariants this package enforces

- **Never import `internal/parser/golang`.** That back-edge is the cycle
  issue #6774 exists to remove. Anything this package needs from the old flat
  package lives in `internal/parser/golang/symbols` — import that instead.
- This package MAY import `internal/parser/golang/deadcode/semantic` (parent
  importing child). `semantic` must never import `deadcode` back.
- A root-kind match is additive and conservative: a name or shape that is not
  confidently recognized contributes nothing, never a guessed root kind. False
  negatives (missed roots) are acceptable; false positives are not.
- Registration and signature matching operate on lower-cased, whitespace-
  compacted text (`goCompactSource`/`goCompactSignature`) so matching is
  resilient to formatting but exact on identifier casing collisions are
  avoided by comparing against resolved import aliases, not bare package
  names.
- `Evidence`'s composition order (registrations, then direct-method-call
  roots, then semantic roots) is a characterization property locked in by
  root's `TestDeadCodeCrossKindSameKeyOrdering` — do not reorder the calls in
  `Evidence` without updating that test's expectations deliberately, never as
  a side effect.
- Do not rewrite a function body while moving code into or out of this
  package. The accuracy golden gate and a parser equivalence dump compare
  emitted facts; a behavior change here is a defect, not a refactor.

## Common changes and how to scope them

- Recognize a new registration shape (a new HTTP router or CLI framework):
  add a matcher in `registrations.go` following the existing
  `goCollectHTTPRegistrationRoots`/`goCollectCobraLiteralRoots` pattern, keyed
  by resolved import alias, never a bare package name.
- Recognize a new root-worthy signature shape: extend `roots.go`'s
  `goSignatureMatches*` family and `RootKinds`, following the existing
  `goSignatureMatchesHTTPHandler`/`goSignatureMatchesCobraRun` pattern.
- Change what `EvidenceSet` exposes: `FunctionRootKinds`/
  `InterfaceRootKinds`/`StructRootKinds` are the only fields
  `language.go` reads. Widening this struct means updating both `README.md`'s
  exported surface and `language.go`'s repoint together.

## Failure modes and how to debug

- A registration is missed: check whether the accessing alias resolved via
  `symbols.AliasesForImportPath` — an alias behind a re-export or a dot-import
  is a known limit, not a bug to chase blindly.
- A false-positive root kind: check whether the matcher compares against a
  resolved import alias (correct) or a bare literal package name (wrong —
  will false-positive on an unrelated identically-named local variable).
- `TestDeadCodeCrossKindSameKeyOrdering` fails after a change to `Evidence`:
  the composition order changed. Confirm the reorder is intentional before
  updating the test's expected order — a silent reorder changes which
  root-kind string a consumer sees first for a key with multiple kinds.

## Do not change without review

- The registration/signature matchers' reliance on resolved import aliases
  rather than bare package-name text matching.
- `Evidence`'s call order into registrations, direct-method-call roots, and
  `semantic.CollectRoots`.
