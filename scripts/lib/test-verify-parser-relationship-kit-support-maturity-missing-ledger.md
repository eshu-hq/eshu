# Parser Support Matrix

| Parser | Parser Class | Grammar Routing | Normalization | Framework Or Root Evidence | Modeled Evidence | Query Surfacing | Real-Repo Validation | End-to-End Indexing |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| CloudFormation | `DefaultEngine (yaml)` | - | - | unsupported | template/resource evidence only | - | - | - |
| Dockerfile | `DefaultEngine (dockerfile)` | - | - | unsupported | build-manifest evidence only | - | - | - |
| HCL | `DefaultEngine (hcl)` | supported | supported | non-code evidence | Terraform and Terragrunt evidence | supported | supported | supported |
| YAML | `DefaultEngine (yaml)` | - | - | unsupported | declarative-data evidence only | - | - | - |
