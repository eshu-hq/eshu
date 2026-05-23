# AGENTS.md - services/cloudfront

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep CloudFront AWS access behind `Client`; the scanner package must not
  import the AWS SDK.
- Keep the boundary `awscloud.ServiceCloudFront` and the configured global
  region label.
- Emit metadata-only reported evidence. Do not infer workload, environment,
  repository, ownership, or deployable-unit truth.
- Do not read object contents, origin payloads, policy documents, certificate
  bodies, private keys, or mutation APIs.
- Preserve origin custom header names only; never persist header values.
- Keep distribution IDs, ARNs, aliases, tags, custom headers, WAF selectors,
  certificate ARNs, and raw AWS errors out of metric labels.
