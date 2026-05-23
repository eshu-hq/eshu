# Product Truth Fixture Contract

This directory is the registry for feature-level truth gates. It proves product
claims with small generic fixtures instead of private repositories or full
dogfood corpora.

| Path | Owns |
| --- | --- |
| `manifest.json` | Owned suite IDs, fixture roots, verifier scripts, expected truth files, and capability names. |
| `expected/*.json` | Assertion contracts for graph, evidence, API, MCP, CLI, or cleanup truth. |
| `dead_iac/` | Generic dead-IaC corpus for Terraform, Helm, Kustomize, Ansible, and Docker Compose reachability. |
| `planned/*.json` | Draft gaps only; entries do not become product claims until `manifest.json` marks the suite owned. |

Each owned suite must include positive cases, negative cases, and ambiguous
cases when the feature can over-admit truth.

Run the static registry gate with:

```bash
./scripts/verify_product_truth_fixtures.sh
```
