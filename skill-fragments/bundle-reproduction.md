---
id: bundle-reproduction
version: 1.0.0
byte_citation: docs/public/reference/evidence-bundle.md#1-49
description: |
  A recipient can run eshu evidence bundle validate against a redacted
  evidence_bundle.v1. The bundle is share-safe; reproduce handles
  point at bounded CLI/API/MCP calls the recipient can run against
  their own instance.
---

# Eshu Bundle Reproduction

An `evidence_bundle.v1` is the portable Eshu evidence unit. It is
designed to be share-safe, deterministic, and reproducible.

A recipient can run `eshu evidence bundle validate` against a
redacted `evidence_bundle.v1` to confirm the bundle shape, the
provenance chain, and the cited fragments.

The bundle is share-safe by construction:

- No private endpoints, credentials, raw prompts, or local paths
  appear in the bundle.
- Provenance is the durable fact row, not the collection
  environment.
- Reproduce handles point at bounded CLI/API/MCP calls the
  recipient can run against their own instance.

A bundle that fails `validate` is not shareable. Treat a bundle
that ships private material as a wire-contract bug.
