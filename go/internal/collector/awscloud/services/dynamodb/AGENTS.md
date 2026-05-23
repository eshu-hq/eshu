# AGENTS.md - services/dynamodb

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep DynamoDB AWS access behind `Client`; the scanner package must not import
  the AWS SDK.
- Emit reported table metadata, tags, TTL, stream settings, capacity, table
  class, replicas, backup status, and direct KMS evidence only.
- Do not read table items, stream records, exports, backup payloads, resource
  policies, PartiQL output, or mutation APIs.
- Treat optional TTL throttling as warning-worthy partial metadata, not a whole
  claim failure, when table facts remain available.
- Do not infer workload, environment, repository, ownership, or deployable-unit
  truth from names, tags, accounts, or aliases.
- Keep table names, ARNs, tags, KMS IDs, stream ARNs, raw AWS errors, and page
  tokens out of metric labels.
