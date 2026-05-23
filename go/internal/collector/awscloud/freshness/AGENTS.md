# AGENTS.md - internal/collector/awscloud/freshness

Read `README.md`, `doc.go`, `types.go`, `eventbridge.go`, `../README.md`, and
`../../../workflow/README.md` before editing freshness trigger behavior.

## Mandatory Rules

- Treat AWS freshness events as wake-up signals only; they never prove graph,
  workload, deployment, resource, or freshness truth.
- Keep every target bounded to one account, one region, and one supported
  service kind. Do not add wildcard support.
- Preserve `aws-global` normalization for IAM, Route 53, and CloudFront.
- Do not add AWS SDK calls, credentialed reads, graph writes, or direct scanner
  execution here.
- Do not bypass workflow claims or make freshness triggers authoritative over
  scheduled scans.
- Keep resource ARNs, names, IDs, tags, raw events, and raw AWS errors out of
  metric labels.
- Add event kinds with tests and bounded telemetry label docs.
