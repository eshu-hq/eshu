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
`scanner_worker.analysis` is emitted by
`go/internal/collector/scannerworker/imageanalyzer`. `scanner_worker.warning`
has two producers: the same image analyzer (carrying image identity and
extraction evidence) and `go/internal/collector/scannerworker`'s
`WarningAnalyzer` fallback, which runs when no concrete analyzer source is
configured and has only the claim's target scope.

`Analysis` required fields match the image analyzer's unconditional payload
fields. `Warning` required fields are only the common core every warning
carries — analyzer, target identity, reason, and the bounded status fields.
The image-analysis fields (image reference/digest, evidence source, extraction
reason) are optional because the `WarningAnalyzer` fallback legitimately lacks
them; making them required would dead-letter every fallback warning as
`input_invalid`. The remaining optional fields are environment attributes that
may be unavailable for an unsupported target or a partially identified image.

Regenerate schemas after changing a struct:

```bash
cd sdk/go/factschema
go generate ./...
cp schema/scanner_worker.*.v1.schema.json fixturepack/schema/
```
