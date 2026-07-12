<!-- docs-catalog
title: Add An Odù
description: Walks through registering a new Ifá conformance case without hand-writing expectations.
type: how-to
audience: practitioner
entrypoint: true
landing: false
-->

# Add an Odù

An Odù is a conformance case: a set of `facts.Envelope` inputs, either from a
recorded cassette or a synthetic generator. You never hand-write what an Odù
should prove — expectations derive from the fact-kind registry and the B-12
snapshot. This mirrors the parser package's "add a language" checklist.

## 1. Declare the input

Choose one of two shapes. Both are cassette-backed in the sense described in
[Odù and cassettes](../concepts/ifa-conformance-platform.md#odu-and-cassettes):

- Drop a **v1 cassette** under `testdata/cassettes/`. The format is
  fail-closed: a non-v1 cassette is rejected at load time.
- Add a **`LoadFacts`/synth descriptor** that produces the Odù's facts in Go
  code. `demoOrgRoundtripOdu` (`go/internal/ifa/roundtrip.go`) and the
  `synth/gcp` generator are the two existing patterns to copy.

To generate a synthetic multi-scope cassette instead of hand-authoring one:

```bash
cd go
go run ./cmd/ifa synth-cassette \
  -seed 4396 \
  -projects 8 \
  -resources 64 \
  -out /tmp/my-odu.json
```

This prints the scope and fact counts it generated, for example
`scopes=8 facts=1248`, so you can confirm the cassette is non-empty before
registering it.

## 2. Redact by key name only

Cassette redaction matches by key name; payloads are otherwise opaque. A
secret that a key-name rule cannot strip must not be in the fixture — there is
no value-content masking to fall back on.

## 3. Register the Odù

Add a `CatalogOdu{Odu: Odu{Name: "odu:<name>", Facts: ...}}` entry to
`catalogSeed` in `go/internal/ifa/catalog_seed.go`. Prefer building facts from
`fixturepack.ValidPayload` examples (see `awsPackOdu`) so the Odù stays in
lockstep with the payload schemas instead of drifting from them.

## 4. Do not hand-list expectations

`Derive` enumerates one surface per fact-kind-registry entry and one per
B-12 evidence-narrowed correlation. Coverage is computed against what your
Odù's facts actually produce, never asserted by name.
`coverage_falsegreen_test.go` proves this the hard way: it deliberately binds
a correlation to the wrong Odù and asserts the coverage check catches it.

## 5. Bind the surfaces your Odù proves

Add a row to `specs/ifa-coverage-manifest.v1.yaml`:

```yaml
- surface: "fact_kind:<kind>"
  scenario: odu
  ref: "odu:<name>"
```

or for a narrowed correlation:

```yaml
- surface: "narrowed_correlation:<rc-id>"
  scenario: odu
  ref: "odu:<name>"
```

Seed a row only once it is genuinely green. An aspirational binding that has
not been proven stays on the uncovered worklist instead of claiming coverage
it does not have.

## 6. Run `make prove`

```bash
make prove
```

This reconciles coverage against the manifest, so a new fact kind or surface
cannot land uncovered by accident, and runs the determinism matrix over your
Odù when Docker is present. The flake policy is absolute: a nondeterministic
failure is a determinism defect. Root-cause it — never retry to green, and
never lower the worker count to make it pass.

## 7. Document what changed

If your Odù introduces a new fact kind or graph surface, document it in the
same change: the fact-kind registry entry and the relevant package README,
the same way the parser model documents a new language.

## Verify

```bash
cd go && go test ./internal/ifa/... -count=1
make prove
```

See [Run the proof suite](run-the-proof-suite.md) for what each step checks,
and the full checklist in `go/internal/ifa/AGENTS.md` for edge cases this
guide compresses.
