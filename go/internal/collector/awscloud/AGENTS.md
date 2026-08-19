# AGENTS.md - internal/collector/awscloud guidance

## Read First

1. `README.md` - package purpose, exported surface, and invariants.
2. `types.go` - `CollectorKind` and the shared observation contracts
   (`Boundary`, `ResourceObservation`, `RelationshipObservation`,
   `ImageReferenceObservation`, `DNSRecordObservation`, `DNSAliasTarget`,
   `DNSRoutingPolicy`, `DNSGeoLocation`, `WarningObservation`).
3. `constants_<service>.go` (one file per AWS service slice, plus
   `constants_common.go` for cross-service targets like `ResourceTypeAWSAccount`
   and `guardduty_types.go` for the GuardDuty slice) - service, resource type,
   and relationship constants. New service constants MUST land in their own
   `constants_<service>.go` sibling, not back in `types.go`, so the 500-line
   cap stays satisfied.
4. `apicall.go` and `scan_status.go` - bounded API-call accounting and
   durable scan-status contracts.
5. `redaction.go` - AWS launch sensitive-key/provider policy and shared
   redaction payload helper.
6. `envelope.go` - durable fact-envelope construction and validation.
7. Service package docs under `services/` before changing scanner-specific
   behavior.
8. `docs/public/services/collector-aws-cloud.md` - AWS collector
   source-truth, claim, and credential contract.
9. `docs/public/guides/collector-authoring.md` - general collector fact
   contract.

## This Directory Is Over The File Cap And Will Be Restructured

The `dirgate` linter caps a package directory at 40 non-test `.go` files. This
one holds **154** and is currently held open by a row in
`scripts/lib/dirgate-grandfather.tsv` (the source of truth;
`tools/golangci-lint-dirgate/grandfather.go` is generated from it). That row is
a stopgap, not an endorsement — it exists so the gate could land green on the
pre-existing sprawl it was written to stop. It is scheduled to be removed, not
renewed: the target is zero grandfathered directories.

The restructure is in scope under epic **#6053** (package restructure — flat
thousand-file directories become a navigable tree), whose end state is that
every package directory holds at most 40 non-test files, named so the directory
tells you the domain and the prefix tells you the family. Nested directories are
expected. This directory's layout will change; do not treat the current shape as
settled. No child issue covers this directory yet — the epic's eleven
workstreams are scoped elsewhere — so "in scope" is the accurate word, not
"tracked".

### Measurements a restructure needs

Gathered while scoping this, and recorded here so the next person does not
re-derive them. **Measured against `2af1e3567`**; the branch has since been
rebased and now forks from main at `606f404f9`, with the figures re-derived
unchanged at that base. The commits in between touch only docs and
`go/internal/query/`, adding no file to this directory and no importer of this
package, which is why none of the figures move. All commands run from the
repository root.

They do not all go stale together, and the difference decides which you can
trust:

- **Rows 1 and 2** move when a `constants_<service>.go` file is added — which
  the Common Changes section actively invites — and **the `dirgate` row pinning
  154 goes red at the same moment**. That is a tripwire you already own: when
  you re-pin it, these two are stale too.
- **Row 3** moves then as well, *and* on any edit to an existing constants file
  — which is the more routine change, since adding a resource type to a service
  that already has a file edits that file rather than creating one. `dirgate`
  cannot see that: its digest is `sha256` over sorted **basenames**, not
  contents, so the count stays 154, the digest is unchanged, and the gate stays
  green while this number drifts.
- **Row 4** is a difference between rows 1 and 2, so it moves only on a file that
  matches row 1's glob but not row 2's: it holds at 21 until a **non-test `.go`
  file that does not match `constants_*.go`** appears in the root. A new
  `*_test.go` file is excluded from both rows' **measures** — row 1 counts
  non-test files, and rows 2 and 3 filter `_test.go` out (see below) — so it
  leaves row 1 and therefore row 4 untouched. Stated by measure rather than by
  glob deliberately: `constants_foo_test.go` *does* match the raw
  `constants_*.go` glob, which is the case the filter paragraph below exists to
  warn about.

  **The 21 are not all non-constants, and the row's label is not a bucket
  boundary.** `acm_types.go`, `cloudtrail_types.go` and `guardduty_types.go` hold
  service and resource constants under the older `_types.go` naming, and land in
  this row only because they do not match `constants_*.go`. Despite the name,
  all three declare **zero** Go types and zero funcs — they are constants files
  whose name predates the convention.

  Re-derive that set by **content, not by filename**: across the 21, exactly
  three declare constants while declaring no types and no funcs. Seven other root
  files also declare constants (`partition.go`, `redaction.go`,
  `iam_permission_envelope.go`, `s3_external_principal_grant_envelope.go`,
  `scan_status.go`, `security_group_rule_envelope.go`, `types.go`) and every one
  of them carries types or functions and is genuinely infrastructure with an
  incidental constant. A `*_types.go` glob is **not** a sound check here —
  `types.go` does not match it, and a constants-only file under any other name
  would be missed the same way. A restructure planner reading "everything else" as
  "infrastructure" would undercount the constants population to consolidate by
  three and misclassify these as infra.
- **Row 5 and the composition** move whenever a new **file** imports this
  package — not a new package. The 1,312 files sit in 413 directories, so most
  of them are second-or-later files in a package that already imports; one more
  file under `services/ec2/` moves row 5 with nothing new importing anything.
  **Nothing catches this**: `dirgate` watches this directory's file set, and a
  new importer lives somewhere else entirely.

So the tripwire covers rows 1, 2 and 4, half of row 3, and none of row 5.
Re-derive row 3, row 5 and the composition before relying on them.

Rows 2 and 3 filter `_test.go` out deliberately, and the filter is load-bearing
rather than tidy. `dirgate` counts non-test files only, so row 1 excludes them
— but the glob `constants_*.go` matches `constants_ec2_test.go`, because `*`
matches `ec2_test`. Without the filter, rows 2 and 3 would count a population
row 1 does not, and row 4 would subtract one population from another. No such
file exists today, so every number here is the same with or without it; the
filter is what stops that being luck. This package names its tests
`envelope_test.go` and `resource_type_contract_test.go` rather than
`constants_<service>_test.go`, which
is a convention and not a rule — and the Common Changes section invites adding
`constants_<service>.go`, so the first contributor who follows Go's default
naming alongside it lands exactly here.

| Measure | Value | How |
| --- | ---: | --- |
| Non-test `.go` files | 154 | `scripts/verify-dirgate.sh --digest internal/collector/awscloud` |
| `constants_<service>.go` files | 133 | `ls go/internal/collector/awscloud/constants_*.go \| rg -v '_test\.go$' \| wc -l` |
| Lines across those | 7,283 | `wc -l $(ls go/internal/collector/awscloud/constants_*.go \| rg -v '_test\.go$') \| tail -1` |
| Non-test root files not matching `constants_*.go` (**not** all non-constants — see row 4 above) | 21 | 154 − 133 |
| Files importing this package | 1,312 | `rg -l --type go '"github.com/eshu-hq/eshu/go/internal/collector/awscloud"' go/ \| wc -l` |

That 1,312 is not 1,312 external dependents, and the difference decides the
restructure risk: **1,243 (94.7%) are inside this package's own subtree**, 69
are outside it, and of those only **five are non-test** —
`go/internal/coordinator/aws_scheduled_scheduler.go`,
`go/cmd/collector-aws-cloud/config.go`,
`go/cmd/collector-aws-cloud/status_committer.go`,
`go/internal/collector/contracttest/contracttest.go`, and
`go/internal/storage/postgres/aws_scan_status.go`.

Three properties of that import surface matter for planning a move:

- **Every dependent imports the package directly.** All 1,312 matching lines are
  the identical bare import — zero dot-imports, zero blank imports, zero aliases
  (`sort | uniq -c` over the extracted lines gives one form, 1312×). So the
  import set *is* the dependent set.
- **There is one package-level re-export, and it IS consumed — in-package.**
  `awsruntime/types.go:27-36` re-exports `WarningAssumeRoleFailed`,
  `WarningBudgetExhausted`, `WarningThrottleSustained`, and
  `WarningOrganizationsOrgAccessSkipped` in a `const` block. No code outside
  `awsruntime` uses the re-exported names — `rg -n 'awsruntime\.Warning[A-Z]' .`
  exits 1 repo-wide — but `awsruntime/source.go:89` uses
  `WarningAssumeRoleFailed` unqualified in production, with four more uses in
  that package's tests. Find those with
  `rg -n '\bWarning(AssumeRoleFailed|BudgetExhausted|ThrottleSustained|OrganizationsOrgAccessSkipped)\b' go/internal/collector/awscloud/awsruntime/`.
  That returns eighteen lines, not five: eight are the `const` block and its
  doc comments, and five are `awscloud.`-qualified references in
  `scan_status.go` and one test — the leading `\b` sits between the `.` and the
  `W`, so it matches the qualified form too. **The re-export uses are the five
  unqualified ones.**

  The qualified-selector search alone is not evidence of non-use, and reaching
  for it is the trap here: by Go scoping, `awsruntime.Warning*` can never appear
  inside package `awsruntime`, which is the one package where these names live
  and are used, so that search exits 1 whether or not they are consumed. Do not
  delete `types.go:27-36` as dead. The blast-radius count above is unaffected —
  those uses are already among the 1,243 — but a move of the `Warning*`
  constants has to carry that file.
- **Cross-service references are common**, because these are cross-resource
  relationship constants. `services/ec2/volume.go` references
  `awscloud.ResourceTypeEC2Volume` (:42) and `awscloud.ResourceTypeKMSKey` (:88)
  in the same file. A service-aligned move therefore is not a uniform rewrite:
  same-service references become local, cross-service ones still need an import.
  Plan for uneven churn.

Count imports, not symbol references. `rg
'awscloud\.(ResourceType|Service|Relationship)[A-Z]'` looks like the more direct
measure and is wrong in both directions at once: without `--type go` it also
matches ~280 package `README.md`/`AGENTS.md` files; restricted to Go it still
counts **12** files under `go/` whose only mention is a comment or a string
literal (seven under `go/internal/reducer/`, one under
`go/internal/storage/postgres/`, and four inside this package's own subtree — of
which `go/internal/collector/awscloud/internal/relguard/relguard.go:42` is a map
value rather than a comment, and just as non-dependent), or 13 counting
`sdk/go/factschema/aws/v1/resource_types.go`, which the command reaches when run
from the repository root; and it *undercounts*, because it misses the
`Warning*`, `TargetType*`, and `IAMPolicySource*` families along with the rest
of the 50 exported constants outside the three prefixes it names.

### Until then

The `constants_<service>.go` convention below still governs new work: a new AWS
service adds its own sibling file. Do not consolidate them ad hoc as a
cap-reduction tactic — the restructure will move them deliberately, and an
interim regrouping would have to be undone. What the seam actually is remains
open: the cross-service references above mean a service-aligned split is uneven
rather than clean, and #6053 carries no design for this directory yet.

## Invariants

- AWS cloud data is reported source evidence. Do not materialize graph truth in
  this package.
- Keep the claim boundary explicit: account, region, service kind, scope,
  generation, collector instance, and fencing token.
- Preserve generation-specific `FactID` values and source-stable
  `StableFactKey` values.
- Never put secrets, session tokens, presigned URLs, full policies, tags, ARNs,
  or resource names in metric labels.
- Keep `APICallEvent` low-cardinality. It may carry service, account, region,
  operation, result, and throttle state only.
- Redact ECS task-definition environment values through `RedactString` before
  persistence; preserve secret `value_from` references without resolving them.
- Redact Lambda function environment values through `RedactString` before
  persistence; preserve image URI, alias, event-source, execution-role, subnet,
  and security-group evidence without inferring workload truth.
- Keep the AWS redaction payload versioned with `RedactionPolicyVersion`.
  Unknown environment keys fail closed as `unknown_provider_schema`; known
  sensitive key names use `known_sensitive_key`.
- Preserve EKS OIDC provider, node group, add-on, IAM role, subnet, and
  security group evidence without inferring Kubernetes workload or ownership
  truth.
- Keep SQS message bodies and queue policy JSON out of facts. Redrive metadata
  is allowed only as reported queue attributes and dead-letter queue
  relationship evidence.
- Keep SNS message payloads, topic policy JSON, delivery-policy JSON,
  data-protection-policy JSON, and raw non-ARN subscription endpoints out of
  facts. ARN subscription endpoints may be reported relationship evidence.
- Keep EventBridge event payloads, mutation APIs, event bus policy JSON, target
  input fields, target transformers, HTTP target parameters, and raw non-ARN
  targets out of facts. ARN target endpoints may be reported relationship
  evidence.
- Keep GuardDuty finding bodies, filter criteria expressions, threat intel set
  list contents, IP set list contents, and mutation APIs out of facts.
  Aggregate finding counts by severity and finding type are allowed; full
  finding details are not.
- Keep S3 object inventory, object keys, bucket policy JSON, ACL grants,
  replication rules, lifecycle rules, notification configuration, inventory
  configuration, analytics configuration, and metrics configuration out of
  facts. Server-access-log target buckets may be reported relationship
  evidence.
- Keep RDS database connections, database names, master usernames, passwords,
  snapshots, log contents, Performance Insights samples, schemas, tables, and
  row data out of facts. RDS dependency edges are reported metadata only.
- Keep DynamoDB item values, table scans, table queries, stream records,
  backup/export payloads, resource policies, PartiQL output, and mutations out
  of facts. DynamoDB table metadata and KMS dependency edges are reported
  metadata only.
- Keep CloudWatch Logs log events, log stream payloads, Insights query results,
  export payloads, resource policies, subscription payloads, and mutations out
  of facts. CloudWatch Logs log group metadata and KMS dependency edges are
  reported metadata only.
- Keep CloudFront object contents, origin payloads, distribution config
  payloads, policy documents, certificate bodies, private keys, origin custom
  header values, and mutations out of facts. Distribution metadata, tags, and
  directly reported ACM certificate and WAF web ACL edges are reported metadata
  only.
- Keep API Gateway execution, exports, API keys, authorizer secrets, policy
  JSON, integration credentials, stage variable values, template bodies,
  payloads, and mutations out of facts. API Gateway API, stage, domain, mapping,
  certificate, access-log destination, and ARN-addressable integration edges are
  reported metadata only.
- Keep Secrets Manager secret values, version payloads, resource policy JSON,
  external rotation partner metadata, external rotation role ARNs, and mutations
  out of facts. Secret metadata, tags, KMS key dependencies, and rotation Lambda
  dependencies are reported metadata only.
- Keep SSM parameter values, history values, raw descriptions, raw allowed
  patterns, raw policy JSON, decrypted content, and mutations out of facts.
  Parameter metadata, tags, safe policy type/status metadata, and KMS key
  dependencies are reported metadata only.
- Keep Athena StartQueryExecution, StopQueryExecution, query result rows,
  query execution result location object contents, named-query SQL bodies,
  prepared-statement query bodies, query history strings, and mutation APIs
  out of facts. Workgroup, data catalog, prepared-statement, and named-query
  metadata plus workgroup-to-S3-result-bucket, workgroup-to-KMS-key,
  prepared-statement-to-workgroup, and named-query-to-workgroup relationship
  evidence are reported metadata only.
- Keep Glue job script bodies, job default-argument values, secret-shaped
  argument keys, connection passwords, connection JDBC credential URLs,
  connection property values, table column statistics with sample values,
  classifier custom patterns, workflow graph payloads, workflow run state, and
  mutations out of facts. Database, table, crawler, job, trigger, workflow,
  and connection metadata plus reported table-in-database,
  table-to-S3-location, crawler-to-database, crawler-to-IAM-role,
  job-to-IAM-role, and trigger-to-job relationships are reported metadata
  only. The Glue SDK adapter must call GetConnections with HidePassword=true
  and GetWorkflow with IncludeGraph=false.
- Keep ElastiCache AUTH tokens, user passwords, user access strings, cache
  keys, cache values, snapshot data, and mutations out of facts. Cache cluster,
  replication group, parameter group, subnet group, user, and user group
  metadata, snapshot name/source/status, and reported VPC, subnet, KMS,
  replication-group-cluster, and user-group-user edges are reported metadata
  only. Drop `User.Passwords` and `User.AccessString` at the adapter boundary.
- Keep IAM Access Analyzer external finding bodies, archive-rule filter
  criteria, policy-generation output, and per-action unused-access details out
  of facts. Analyzer metadata, archive-rule names, aggregate finding counts,
  analyzer relationships, and per-resource unused-access last-accessed
  summaries are reported metadata only.
- Keep Organizations policy document bodies, statements, conditions, action
  lists, account lifecycle mutations, policy mutations, delegated-admin
  mutations, and service-access mutations out of facts. Organization roots,
  OUs, accounts, policy summaries, target bindings, and delegated
  administrators are reported metadata only. Redact account email and account
  name values through `RedactString` before persistence.
- Keep ELBv2 target health out of facts; it is live/noisy state, not stable
  topology truth.
- Keep EC2 instance inventory out of the EC2 scanner; ENI attachment target
  ARNs are metadata only.
- Keep EC2 EBS volume facts source-only. They may report encrypted/KMS and
  attachment metadata from `DescribeVolumes`, but reducers own block-device/KMS
  posture truth.
- Keep AWS SDK calls out of this package. Runtime adapters own SDK pagination,
  retries, throttling, and credential loading.

## Common Changes

- Add a new AWS service by creating a new `constants_<service>.go` sibling
  (one file holds the `Service<X>`, `ResourceType<X>...`, and
  `Relationship<X>...` constants for that slice), a service package under
  `services/`, scanner tests, a service `awssdk` adapter, package docs, and a
  branch in `awsruntime.DefaultScannerFactory`. Do not grow `types.go` with
  new service-specific constants; it stays at the shared observation
  contracts only.
- For that new service package, include `doc.go`, `README.md`, and `AGENTS.md`
  before merge and run `scripts/verify-package-docs.sh`.
- If the service adds pagination fanout, claim concurrency, batch sizing,
  queue pressure, or downstream graph/materialization pressure, run
  `scripts/verify-performance-evidence.sh` and add tracked
  Performance Evidence plus Observability Evidence markers naming the
  input shape, queue/resource counts, and exact metrics/spans/logs/status
  fields.
- Add a new fact envelope only after `internal/facts` exposes the fact kind and
  schema version.
- Add redaction or credential rules at the runtime boundary unless the value is
  part of the durable envelope contract.

## What Not To Change Without An ADR

- Do not make this package call AWS APIs directly.
- Do not add graph writes, reducer admission, or query behavior here.
- Do not infer environment, workload, ownership, or deployable-unit truth from
  names, tags, folders, or account aliases in this package.
