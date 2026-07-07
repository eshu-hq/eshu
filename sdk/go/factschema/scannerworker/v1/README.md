# Scanner Worker Fact Payloads (schema version 1)

This package holds typed payload structs for the `scanner_worker` fact family.
The parent `factschema` package provides the public decode/encode seam:

| Fact kind | Struct | Decode function |
| --- | --- | --- |
| `scanner_worker.analysis` | `Analysis` | `factschema.DecodeScannerWorkerAnalysis` |
| `scanner_worker.warning` | `Warning` | `factschema.DecodeScannerWorkerWarning` |

`scanner_worker.analysis` records successful image analysis coverage.
`scanner_worker.warning` records explicit unsupported or unscanned image
evidence. Both kinds feed the supply-chain impact/readiness surfaces and are
emitted by `go/internal/collector/scannerworker/imageanalyzer`.

Required fields match the image analyzer's unconditional payload fields.
Optional fields are environment attributes that may be unavailable for an
unsupported target or a partially identified image.

Regenerate schemas after changing a struct:

```bash
cd sdk/go/factschema
go generate ./...
cp schema/scanner_worker.*.v1.schema.json fixturepack/schema/
```
