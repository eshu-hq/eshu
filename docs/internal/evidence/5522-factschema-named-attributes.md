# #5522 factschema named `Attributes` decode

## Result

The factschema map decoder now distinguishes a fully typed field named
`Attributes` from the polymorphic pass-through remainder by its wire contract.
Only an exact `Attributes map[string]any` field tagged `json:"-"` captures
unmodeled payload keys. A field tagged `json:"attributes,omitempty"` owns the
single `attributes` payload key like any other named field.

This corrects the decoded shape for:

- `terraformstate/v1.Resource`;
- `aws/v1.Warning`; and
- `secretsiam/v1.CoverageWarning`.

The Terraform projector's issue-specific shape unwrap is removed. It now uses
the correctly decoded `Resource.Attributes` map directly.

## Root cause and TDD reproduction

Root-Cause Evidence: before the implementation changed, the focused regression
test returned exit code 1 for all three public decoders. Each decoded
`Attributes` value as:

```text
{"attributes": {<the real object>}, "unknown_top_level": <unrelated value>}
```

instead of the real object. The same pre-fix test proved that
`WithoutAttributesRemainder()` incorrectly left an explicitly tagged named
field nil.

The observed cause was the cached-plan builder in `decode_map.go`: it diverted
every field whose Go name was `Attributes` and whose kind was any map before it
parsed the field's JSON tag. The named `attributes` key was therefore absent
from both the assignment plan and known-key set, so the later remainder loop
captured it and every unrelated top-level key.

The failing command was:

```bash
cd sdk/go/factschema
go test . \
  -run '^(TestDecodeExplicitlyTaggedAttributesField|TestDecodeMapIntoWithDoesNotSkipNamedAttributes)$' \
  -count=1
```

After the decoder fix, the same tests pass. Additional cases pin these
boundaries:

- absent and explicit-null optional attributes remain nil;
- a present empty object remains a non-nil empty map;
- a scalar in place of the object fails closed as `input_invalid`;
- `json:"-,omitempty"` is the literal `-` named key, matching
  `encoding/json`, not a remainder marker; and
- unrelated top-level keys are ignored by fully typed payloads rather than
  leaking into their named `Attributes` field.

## Remainder compatibility

The intentional open-object structs retain exact `json:"-"` tags. Their
default decodes still collect otherwise-unmodeled keys, while
`WithoutAttributesRemainder()` still skips only that remainder allocation.
The complete standalone module test covers the AWS, Azure, GCP, and codegraph
remainder types together with the generated-schema drift locks.

No payload struct, JSON tag, fact kind, schema version, or generated JSON
Schema changed. `go generate ./...` completed with no generated diff.

## Projector and golden truth

The corrected flow is:

```text
Envelope.Payload["attributes"]
  -> DecodeTerraformStateResource
  -> terraformstate/v1.Resource.Attributes
  -> TerraformStateResourceRow.Attributes
  -> allowlisted Terraform graph properties
```

The ordinary two-key provider map and a hostile legitimate map whose sole key
is itself named `attributes` both reach the canonical row byte-for-byte. The
old workaround failed the hostile case after the decoder was corrected because
it unwrapped that legitimate key; deleting the workaround made the test pass.

The full B-7 run completed with:

```text
summary: 525 pass, 0 required-fail, 0 advisory-warn
PASS: B-7 golden corpus gate green (elapsed 134s, budget ceiling 1800s)
```

All projector/reducer drains were terminal with zero dead letters. No cassette,
B-12 snapshot, or timing-baseline change was required.

## Performance

Performance Evidence: the same warmed steady-state polymorphic
`BenchmarkDecodeAWSResource` ran five times before and after the change on the
same host and base commit.

| variant | median | bytes/op | allocs/op |
| --- | ---: | ---: | ---: |
| pre-fix | 367.6 ns/op | 553 | 7 |
| fixed | 367.4 ns/op | 553 | 7 |

The measured difference is -0.2 ns (-0.05%) with identical allocation work.
The stricter type/tag classification runs only once while constructing the
cached plan for a struct type. Steady-state intentional remainder decoding is
unchanged; explicitly tagged attributes now use the existing direct map
assignment rather than allocating and copying a remainder map.

Benchmark command:

```bash
cd sdk/go/factschema
go test . -run '^$' -bench '^BenchmarkDecodeAWSResource$' -benchmem -count=5
```

## Verification

No-Regression Evidence:

```bash
cd sdk/go/factschema
go generate ./...
go test ./... -count=1
go test . -run '^$' -bench '^BenchmarkDecodeAWSResource$' -benchmem -count=5

cd ../../../go
go test ./internal/projector ./internal/storage/cypher -count=1

cd ..
bash scripts/test-verify-golden-corpus-gate.sh
bash scripts/verify-golden-corpus-gate.sh
git diff --check
```

No-Observability-Change: this correction adds no worker, queue, transaction,
retry, metric, span, log, or status behavior. Malformed named attribute values
continue through the existing classified decode-error and projector quarantine
path. The full live corpus provides the operator-facing terminal-queue and
dead-letter proof for the affected projection path.
