# AGENTS.md - internal/collector/awscloud/services/glue guidance

## Read First

1. `README.md` - package purpose, exported surface, redaction policy, and
   invariants.
2. `types.go` - scanner-owned Glue domain types.
3. `scanner.go` - database, table, crawler, job, trigger, workflow, connection,
   and relationship emission.
4. `helpers.go` - secret-shaped key filter and scanner-side cloning helpers.
5. `relationships.go` - relationship emission rules.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Glue API access behind `Client`; do not import the AWS SDK into this
  package.
- Never run jobs, start crawlers, mutate Data Catalog state, or wire any
  `Create*`, `Update*`, `Delete*`, `StartJobRun`, `BatchStopJobRun`, or
  `StartCrawler` API.
- Never persist Glue job script bodies, default-argument values,
  default-argument keys that look like secrets, security-configuration secret
  material, or any value that smells like a credential.
- Never persist Glue connection property values. The SDK adapter must request
  `GetConnections` with `HidePassword=true`. Only safe property key names are
  recorded, and secret-shaped keys are dropped by `filterSafeKeys`.
- Never persist Glue workflow graph payloads or run state. The SDK adapter
  must request `GetWorkflow` with `IncludeGraph=false`.
- Never persist Glue table column statistics that contain sample values,
  partition value samples, or any row-level content.
- Never persist classifier custom patterns or other regex/AST payloads that
  may leak detection logic.
- Emit table-to-S3-location relationships only when the storage location
  parses as `s3://bucket/key`. Other storage URI schemes stay as resource
  attributes.
- Emit job-to-IAM-role and crawler-to-IAM-role relationships only when AWS
  reports an ARN-shaped role identity.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from database, table, job,
  trigger, workflow, connection names, or AWS tags.
- Preserve stable database, table, crawler, job, trigger, workflow, and
  connection identities across repeated observations in the same AWS
  generation.
- Keep Glue resource ARNs, names, parameters, tags, schedules, and AWS error
  payloads out of metric labels.

## Common Changes

- Add a new Glue metadata field by extending the scanner-owned type, writing a
  focused scanner or adapter test first, then mapping it through `awscloud`
  envelope builders. If the field can carry credential or sample-value
  material, leave it out of the scanner contract until an ADR documents a
  sanitized exception.
- Add new relationship evidence only when the Glue API reports both sides
  directly and the target identity is not sensitive (for example, an ARN or a
  catalog-stable name).
- Add a new secret-shaped key fragment to `secretKeyFragments` when AWS
  exposes a new credential-bearing argument or property name family.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not put events, mutate jobs, mutate crawlers, mutate triggers, mutate
  databases, mutate tables, mutate connections, mutate workflows, or call any
  Glue mutation API.
- Do not call `GetColumnStatisticsForTable`, `BatchGetCustomEntityTypes`,
  classifier custom-pattern reads, `GetUserDefinedFunctions`,
  `GetSecurityConfiguration`, or any API that retrieves secret-shaped values.
- Do not persist Glue tags into facts until tagging is added through an ADR
  that documents the source API and label policy.
- Do not resolve Glue names, parameters, or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
