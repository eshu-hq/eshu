# Scanner Worker Fact Payloads (schema version 1)

This package holds typed payload structs for the `scanner_worker` fact family.
The parent `factschema` package provides the public decode/encode seam:

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `scanner_worker.analysis` | `Analysis` | `factschema.DecodeScannerWorkerAnalysis` |
| `scanner_worker.warning` | `Warning` | `factschema.DecodeScannerWorkerWarning` |

`scanner_worker.analysis` records successful image analysis coverage.
`scanner_worker.warning` records explicit unsupported or unscanned evidence.
Both kinds feed the supply-chain impact/readiness surfaces.
`scanner_worker.analysis` is emitted by the satellite image analyzer.
`scanner_worker.warning` has two producers: the same image analyzer (carrying
image identity and extraction evidence) and the satellite's warning fallback,
which runs when no concrete analyzer source is configured and has only the
claim's target scope.

`Analysis` required fields match the image analyzer's unconditional payload
fields. `Warning` required fields are only the common core every warning
carries — analyzer, target identity, reason, and the bounded status fields.
The image-analysis fields (image reference/digest, evidence source, extraction
reason) are optional because the `WarningAnalyzer` fallback legitimately lacks
them; making them required would dead-letter every fallback warning as
`input_invalid`. The remaining optional fields are environment attributes that
may be unavailable for an unsupported target or a partially identified image.

`Warning` also carries two optional scan-target fields, `repository_id` and
`relative_path`, added for core eshu-hq/eshu#10043. They scope a failed or
partial Go reachability analysis (`analyzer: go_reachability`) to a repository
and to the go.mod file that failed. `relative_path` is the slash-separated path
of the go.mod file relative to the repository root, byte-equal to the
`relative_path` on that file's `vulnerability.go_module_evidence` rows. Blank or
absent means the whole repository, and a warning with no `repository_id` joins
nothing. The file path, not the module path, is the scope: a finding is keyed by
the vulnerable dependency, while a scan is keyed by the go.mod file.

A producer MUST put the failure class in `reason` and MUST leave
`warning_class` as the constant `scanner_worker_warning`; a reader names the
failure from `reason`. A producer MUST NOT write a `go_call_reachability` fact
from the failed scan of a go.mod it warns about; a sibling go.mod's finished
scan still writes its facts, even for the same dependency. A reader applies the rule on
`Warning`'s type documentation: a reachable fact always wins, and any other
fact or no fact beside a joined warning is missing evidence naming
`govulncheck analysis failed: <reason> at <relative_path>`. A `module` or
`unknown` level fact with no joined warning keeps its own state, `module_only`
or `unknown`, with the reason text the reader already attaches to that level;
this contract does not fix that text. The core reducer
does not read this kind yet (eshu-hq/eshu#10043); until that lands a failed scan
stays unknown.

Regenerate schemas after changing a struct:

```bash
cd sdk/go/factschema
go generate ./...
cp schema/scanner_worker.*.v1.schema.json fixturepack/schema/
```
