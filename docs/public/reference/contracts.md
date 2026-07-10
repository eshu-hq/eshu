<!-- docs-catalog
title: Contract Reference
description: Groups durable source contracts for facts, collectors, reducers, query surfaces, and generated references.
type: reference
audience: maintainer, practitioner
entrypoint: true
landing: false
-->

# Contract Reference

Contract reference pages define durable Eshu interfaces and source-of-truth
rules. Use them when changing or auditing behavior that crosses service,
collector, reducer, query, API, MCP, or generated-reference boundaries.

## Start Here

| Need | Reference |
| --- | --- |
| Fact shape and source truth | [Fact Envelope Reference](fact-envelope-reference.md) and [Fact Schema Versioning](fact-schema-versioning.md) |
| Reducer delivery and graph projection guarantees | [Reducer Guarantees](reducer-guarantees.md) |
| API and MCP contracts | [HTTP API](http-api.md), [MCP Reference](mcp-reference.md), and [MCP Tool Contract Matrix](mcp-tool-contract-matrix.md) |
| Environment source of truth | [Environment Variables](environment-variables.md) and [Environment Variable Registry](env-registry.md) |
| Documentation IA metadata | [Docs Catalog Metadata](docs-catalog.md) |
| Collector contracts | [Collector Extraction Policy](collector-extraction-policy.md), [Multi-Cloud Runtime Collector Contract](multi-cloud-collector-contract.md), and provider-specific collector contracts |
| Evidence and answer contracts | [Portable Evidence Bundle](evidence-bundle.md), [Evidence Citation Handle Contract](evidence-citation-handles.md), and [Answer Packet Contract](answer-packets.md) |

## Placement

This area is for exact lookup, not first-run reading. Tutorials and how-to
guides should link here for details instead of copying contract language into
human onboarding pages.

## Generated Reference Ownership

`docs/public/reference/env-registry.md` is generated from
`go/internal/envregistry`. Regenerate it with `bash
scripts/generate-env-registry-doc.sh`; verify drift with `bash
scripts/verify-env-registry-doc.sh`. Do not edit the generated page by hand.
