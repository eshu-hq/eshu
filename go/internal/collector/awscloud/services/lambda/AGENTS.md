# AGENTS.md - services/lambda

Read `README.md`, `doc.go`, `types.go`, `scanner.go`, `relationships.go`, and
`awssdk/README.md` before editing this service.

## Mandatory Rules

- Keep Lambda AWS access behind `Client`; the scanner package must not import
  the AWS SDK.
- Redact environment values before envelope construction.
- Emit reported function, alias, event-source mapping, VPC, log group, layer,
  image, and role relationship evidence only.
- Do not persist presigned code URLs, function code, payloads, raw environment
  values, resource policies, invocation data, or mutation results.
- Do not infer workload, environment, repository, ownership, or deployable-unit
  truth from function names, tags, layers, images, or accounts.
- Keep function names, ARNs, tags, image URIs, environment values, code URLs,
  raw AWS errors, and page tokens out of metric labels.
