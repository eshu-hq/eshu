# AGENTS.md - internal/collector/awscloud/services/amplify guidance

## Read First

1. `README.md` - package purpose, exported surface, redaction policy, and
   invariants.
2. `types.go` - scanner-owned Amplify domain types.
3. `scanner.go` - app, branch, and resource/relationship emission.
4. `relationships.go` - relationship emission rules.
5. `helpers.go` - ARN synthesis, repository-URL sanitization, CloudFront
   extraction, and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Amplify API access behind `Client`; do not import the AWS SDK into this
  package.
- Never create, update, delete, start a job, start a deployment, or mutate any
  Amplify resource, and never wire any `Create*`, `Update*`, `Delete*`,
  `Start*`, or webhook API.
- Never persist Amplify environment variables, build-spec bodies, basic-auth
  credentials, or repository access tokens. The scanner-owned types carry no
  such field; keep it that way.
- Repository URLs are host and path only. Route every repository URL through
  `SanitizeRepositoryURL` so a userinfo token is stripped before it reaches a
  fact payload or a graph join key.
- The app node's `resource_id` is the app ARN (API-reported, or partition-aware
  synthesized when absent). Source every one of the app's own outgoing edges on
  that same id, and target the branch-to-app edge at it.
- Target app-to-IAM-role edges at the role ARN (the IAM scanner `resource_id`).
- Target the app-to-Route 53 edge at the normalized domain root (matching the
  route53 `normalized_name` anchor) with no target ARN; target the
  app-to-CloudFront edge at the `*.cloudfront.net` distribution domain (matching
  the cloudfront domain-name anchor) with no target ARN, and dedupe repeated
  CloudFront domains.
- Derive synthesized-ARN partitions from the boundary via
  `awscloud.PartitionForBoundary`; never hardcode `arn:aws:`.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from app, branch, or domain
  names or AWS tags.
- Keep Amplify resource ARNs, names, tags, and AWS error payloads out of metric
  labels.

## Common Changes

- Add a new Amplify metadata field by extending the scanner-owned type, writing
  a focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry credential, env-var, or build-spec
  material, leave it out of the scanner contract.
- Add new relationship evidence only when the Amplify API reports both sides
  directly and the target identity matches the target scanner's published
  `resource_id` shape (ARN, normalized name, or correlation anchor). Verify the
  shape by reading the target scanner before adding the edge.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not mutate apps, branches, domains, jobs, or deployments, or call any
  Amplify mutation API.
- Do not persist environment variables, build-spec bodies, basic-auth
  credentials, or repository access tokens.
- Do not resolve Amplify names or tags into workload ownership here; correlation
  belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
