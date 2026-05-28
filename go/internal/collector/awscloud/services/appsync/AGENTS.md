# AppSync Service Package

Read these files before editing this package:

1. `README.md`
2. `doc.go`
3. `types.go`
4. `scanner.go`
5. `relationships.go`
6. `helpers.go`
7. `awssdk/README.md`

Keep the scanner metadata-only. The schema SDL body, resolver request/response
mapping templates (VTL or JS), pipeline function code bodies, and API key values
are forbidden payloads. Do not add a field for any of them to the scanner-owned
types, and do not add an attribute key that could carry one. A struct-reflection
test and an attribute-key test enforce this; do not weaken them.

The scanner boundary must remain `awscloud.ServiceAppSync`.

Every relationship must set a non-empty `target_type` and a `target_resource_id`
that matches how the target scanner publishes its resource_id:

- data source to Lambda joins by function ARN under `aws_lambda_function`.
- data source to DynamoDB joins by table ARN (synthesized with a partition
  derived from a source ARN, never hardcoded `arn:aws:`) or the bare table name
  under `aws_dynamodb_table`.
- data source to RDS joins by cluster ARN under `aws_rds_db_cluster`.
- API to Cognito user pool joins by the bare pool ID under
  `aws_cognito_user_pool`, never the compound provider-name string.

OpenSearch and HTTP targets carry the endpoint URL as join evidence and do not
synthesize an ARN.
