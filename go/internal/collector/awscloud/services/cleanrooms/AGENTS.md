# AGENTS.md - internal/collector/awscloud/services/cleanrooms guidance

## Read First

1. `README.md` - package purpose, exported surface, resource_id shapes, and
   invariants.
2. `types.go` - scanner-owned Clean Rooms domain types.
3. `scanner.go` - collaboration, configured-table, and membership resource and
   relationship emission.
4. `relationships.go` - relationship emission rules and join keys.
5. `helpers.go` - resource_id derivation and scanner-side cloning helpers.
6. `../../README.md` - shared AWS cloud observation and envelope contract.
7. `docs/public/services/collector-aws-cloud-scanners.md` - AWS collector
   service coverage and runtime requirements.

## Invariants

- Keep Clean Rooms API access behind `Client`; do not import the AWS SDK into
  this package.
- Never read or persist analysis-rule SQL, protected-query bodies or results,
  allowed-column names (only their count), or member secrets such as the
  Snowflake `SecretArn`. Never call any protected-query/job run, result read, or
  `Create*`/`Update*`/`Delete*` mutation API.
- The collaboration node publishes its resource_id as the collaboration ARN
  (fallback to id). Key the membership-in-collaboration edge on that exact value
  so it joins the collaboration node.
- Emit the configured-table-to-Glue-table edge only when the backing table is a
  Glue table and the Glue table name is present. Key it on the
  `<database>/<table>` resource_id the Glue scanner publishes, falling back to
  just `<table>` when the database name is missing (matching the Glue scanner's
  own table-node fallback so the edge still joins). Leave `target_arn` empty (the
  Glue table node is name-keyed, not ARN-keyed). Skip, never dangle, when the
  Glue table name is missing or the backing table is Athena/Snowflake.
- Every relationship sets a non-empty `target_type` naming a declared
  `awscloud.ResourceType*` constant and a `target_resource_id` matching how the
  target scanner publishes its resource_id.
- Emit reported evidence only. Do not infer deployment, workload, repository
  ownership, environment, or deployable-unit truth from names or AWS tags.
- Trim whitespace on any string used as an id or service_kind.

## Common Changes

- Add a new Clean Rooms metadata field by extending the scanner-owned type,
  writing a focused scanner or adapter test first, then mapping it through
  `awscloud` envelope builders. If the field can carry SQL, query results,
  allowed-column names, or secrets, leave it out of the scanner contract.
- Add new relationship evidence only when the Clean Rooms API reports both sides
  directly and the target identity matches an existing scanner's published
  resource_id shape.
- Extend SDK pagination in the `awssdk` adapter, not here.

## What Not To Change Without An ADR

- Do not read protected-query bodies or results, run protected queries or jobs,
  read analysis-rule or analysis-template bodies, or call any Clean Rooms
  mutation API.
- Do not persist allowed-column names or any Snowflake/Athena secret or
  connection identifier.
- Do not resolve Clean Rooms names or tags into workload ownership here;
  correlation belongs in reducers.
- Do not add graph writes, reducer logic, or query behavior.
- Do not add AWS credential loading or STS calls to this package.
