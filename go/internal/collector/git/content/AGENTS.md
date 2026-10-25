# AGENTS.md — go/internal/collector/git/content

## Read first

1. `go/internal/collector/git/content/README.md` — purpose, exported surface, and the notes below
2. `go/internal/collector/git/content/doc.go` — the package contract
3. `go/internal/collector/repo/git/model/README.md` — the shared types and the fact-stream writer
4. `go/internal/collector/repo/git/README.md` — how the snapshot and fact stream drive this package

## The rule that shapes this package

This package is a leaf. `gitrepo` imports it; it must never import `gitrepo`.
If you find yourself needing something from `gitrepo`, the choice is:

- the thing is genuinely shared -> move it down into `gitmodel`, or
- the thing is snapshot logic that landed in the wrong file -> move it into
  `gitrepo` next to the type it reads.

Adding an import of `gitrepo` here does not compile, and working around that by
duplicating a type is worse than either fix above. That is why
`RepositoryFactEnvelope` takes the precomputed default branch and ref payload
instead of `[]GitRef`.

## Facts are a contract

Every envelope here writes through `gitmodel.FactEnvelope`. Changing what is
emitted — new fact kind, changed payload shape, different counts — changes
projected truth, so it needs the cassettes and the B-12 snapshot updated in the
same change. Load `eshu-golden-corpus-rigor` and `eshu-contract-rigor` before
touching emission.

## Content-store policy (#8812)

`ApplyContentStore` is the only place a policy decision turns into a stored
body. Rules that keep it safe:

- Never return the source body on any failure path. A redactor error, a panic,
  non-UTF-8 input, and an unknown policy all return an empty body.
- Keep the marker `REDACTED_<finding_kind>`. `[redacted]` and `***` are
  rejected (the detector cannot see them). `REDACTED_value` is the skeleton
  mode's marker for a value or line that is no finding; the marker mode never
  writes it.
- The redactor binds to `go/internal/secret/detect`; do not copy its pattern.
  `TestRedactMarkerPerKindKeepsKindUnderMarkerArm` pins that every marked line
  keeps its raw kind under the migration 235 marker arm (it pinned the measured
  migration 138 losses before #8833); do not loosen it.
- Keep `redactMarker` fail-closed for YAML value shapes. A new key shape needs a
  seeded-violation test in `marker_redactor_test.go`. A listed key whose value
  can leave the key's line (flow style, a plain scalar at any indent) is
  covered by `marker_yaml.go`; add the shape to `flowHostileSeeds` and
  keep `TestRedactMarkerNeverStoresADecodedCredential`, the independent
  yaml.v3 oracle, green. The redactor's continuation (`redactInState`) must not
  be the only thing standing between a decoded value and the stored body.
- Keep the URL userinfo rule (`marker_url_userinfo.go`) narrower than the
  credential-key rule only when the value is exactly one readable URL; every
  other shape must fall back to whole-value redaction.
- `redactSkeleton` is an allowlist. Widen the allowlist only for a shape that
  cannot carry a credential, with a seeded case in
  `skeleton_redactor_property_test.go`, and keep the rule that a raw finding
  line keeps its `REDACTED_<finding_kind>` (the property test checks it).
- The lexical state (`skeleton_scan.go`) is hand written on purpose: it needs
  O(line) memory, where `hclsyntax` allocates 16 to 600 times the body. Never
  import `hclsyntax` in a non-test file here. Any change to the scanner must keep
  `TestRedactSkeletonOracle` green; that test compares it with `hclsyntax` on
  generated bodies, and `-skeleton-oracle-bodies=200000` runs the large form. A
  lexing failure (nesting past 32, a line that ends inside a quoted string) is
  sticky and redacts the rest of the body whole; do not add a way out of it.

## Verification

```bash
cd go && go test ./internal/collector/git/content/... ./internal/collector/repo/git/... -count=1
cd go && go vet ./internal/collector/...
```
