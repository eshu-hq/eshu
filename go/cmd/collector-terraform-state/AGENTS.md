# collector-terraform-state Agent Guidance

## Read First

1. `README.md` and `doc.go` for command scope.
2. `config.go` and `config_test.go` for collector-instance, credential,
   redaction, heartbeat, and lease validation.
3. `service.go` for claimed-service and redaction-rule wiring.
4. `target_scope_source_factory.go`, `aws_s3.go`, and `aws_dynamodb.go` for
   approved source and read-only AWS adapters.
5. `go/internal/collector/tfstateruntime/README.md` and
   `go/internal/workflow/README.md` for claim, source, and fencing behavior.

## Local Rules

- Own runtime wiring only. Work-item planning and reconciliation stay in
  `workflow-coordinator`.
- Select exactly one enabled, claim-capable `terraform_state` collector
  instance from `ESHU_COLLECTOR_INSTANCES_JSON`.
- Open only exact configured or approved sources. Do not scan buckets,
  prefixes, workspaces, or guessed local `.tfstate` files.
- Keep raw state bytes inside reader/parser streams. Do not log or document
  bucket/key pairs, local paths, raw state JSON, parsed secrets, or credentials.
- Keep S3 access read-only behind `terraformstate.S3ObjectClient`; SDK adapters
  belong in command wiring, not parser/runtime packages.
- Require redaction key material and a versioned `redact.RuleSet` at startup.
  `service.go` must wire `tfstateruntime.ClaimedSource.RedactionRules` from
  config.
- Use workflow claim fencing from `collector.ClaimedService`; do not create a
  second claim lifecycle or retry fenced mutations locally.

## Change Rules

- Add tests for collector-instance selection, target-scope credential routing,
  redaction rules, S3 freshness, and claim errors when those paths change.
- Treat claims, leases, S3 pagination, parser volume, batching, worker fanout,
  and downstream materialization cost as performance-sensitive.
- Do not move Terraform-state collection into `workflow-coordinator`.
