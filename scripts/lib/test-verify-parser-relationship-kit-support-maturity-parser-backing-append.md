
## Parser Backing Ledger

See `specs/parser-backing-ledger.v1.yaml`.

| Parser Key | Implementation Class | Decision | Evidence |
| --- | --- | --- | --- |
| cloudformation | `structured-parser-backed-exception` | Decoded YAML/JSON plus bounded CloudFormation evaluation is the canonical parser. | `specs/parser-backing-ledger.v1.yaml` |
| dockerfile | `structured-parser-backed-exception` | Dockerfile instruction scanning is the canonical build-manifest parser. | `specs/parser-backing-ledger.v1.yaml` |
| hcl | `structured-parser-backed-exception` | HashiCorp HCL v2 is the canonical Terraform/Terragrunt parser. | `specs/parser-backing-ledger.v1.yaml` |
| yaml | `structured-parser-backed-exception` | YAML v3 document decoding is the canonical declarative-data parser. | `specs/parser-backing-ledger.v1.yaml` |
