# Product Truth Fixture Contract

This directory is the registry for feature-level truth gates.

The goal is to keep a small, generic fixture corpus that proves Eshu product
claims without relying on private repositories or full-corpus dogfood runs.
Each owned capability must identify:

- the fixture roots that exercise it
- the compose or local verifier that proves it
- the expected graph, evidence, API, MCP, or CLI truth surface
- negative and ambiguous cases when the feature can over-admit truth

Large private corpora remain useful for scale and performance. They are not the
source of truth for feature correctness.

## Files

- `manifest.json` lists owned and planned product-truth suites.
- `expected/*.json` describes the current expected assertions for existing
  fixture-backed gates.
- `dead_iac/` is the owned fixture corpus for the dead-IaC capability, with
  Terraform, Helm, Kustomize, Ansible, and Docker Compose used, unused, and
  ambiguous examples.
- `planned/*.json` describes feature gaps that must become owned suites before
  Eshu can claim the capability.

Run the fast static contract check with:

```bash
./scripts/verify_product_truth_fixtures.sh
```
