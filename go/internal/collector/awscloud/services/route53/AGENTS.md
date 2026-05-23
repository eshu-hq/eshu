# AGENTS.md - services/route53

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep Route 53 AWS access behind `Client`; the scanner package must not import
  the AWS SDK.
- Keep Route 53 global and use the configured global region label.
- Emit reported hosted-zone, record-set, tag, and alias relationship evidence
  only.
- Do not infer workload, environment, repository, ownership, deployable-unit, or
  DNS authority truth beyond directly reported Route 53 records.
- Do not add domain registration, health-check payload, resolver query log, or
  mutation behavior here.
- Keep zone IDs, zone names, record names, record values, tags, raw AWS errors,
  and page tokens out of metric labels.
