# Parser Support Matrix

| Parser | Parser Class | Grammar Routing | Normalization | Framework Or Root Evidence | Modeled Evidence | Query Surfacing | Real-Repo Validation | End-to-End Indexing |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| CloudFormation | `DefaultEngine (yaml)` | - | - | unsupported | template/resource evidence only | - | - | - |
| Dockerfile | `DefaultEngine (dockerfile)` | - | - | unsupported | build-manifest evidence only | - | - | - |
| HCL | `DefaultEngine (hcl)` | supported | supported | non-code evidence | Terraform and Terragrunt evidence | supported | supported | supported |
| YAML | `DefaultEngine (yaml)` | - | - | unsupported | declarative-data evidence only | - | - | - |

## Parser Backing Ledger

See `specs/parser-backing-ledger.v1.yaml`.

| Parser | Implementation Class | Decision | Evidence |
| --- | --- | --- | --- |
| cloudformation | `structured-parser-backed-exception` | Decoded YAML/JSON plus bounded CloudFormation evaluation is the canonical parser. | `specs/parser-backing-ledger.v1.yaml` |
| dockerfile | `structured-parser-backed-exception` | Dockerfile instruction scanning is the canonical build-manifest parser. | `specs/parser-backing-ledger.v1.yaml` |
| hcl | `structured-parser-backed-exception` | HashiCorp HCL v2 is the canonical Terraform/Terragrunt parser. | `specs/parser-backing-ledger.v1.yaml` |
| yaml | `structured-parser-backed-exception` | YAML v3 document decoding is the canonical declarative-data parser. | `specs/parser-backing-ledger.v1.yaml` |

## Language Feature Parity Ledger

See `specs/language-feature-parity-ledger.v1.yaml`.
