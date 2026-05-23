# AGENTS.md - cmd/collector-terraform-state guidance for LLM assistants

## Read first

1. `README.md` - runtime purpose, config, and local run notes.
2. `service.go` - claimed service wiring.
3. `config.go` - environment parsing and collector-instance selection.
4. `aws_s3.go` - read-only AWS SDK adapter.
5. `go/internal/collector/tfstateruntime/README.md` - source and claim invariants.
6. `go/internal/workflow/README.md` - collector-instance and claim lifecycle.

## Invariants

- This command owns runtime wiring only. Do not move scheduling or claim
  reconciliation here; that stays in `workflow-coordinator`.
- Select exactly one enabled, claim-capable `terraform_state` collector
  instance from `ESHU_COLLECTOR_INSTANCES_JSON`.
- Keep raw Terraform state bytes inside reader/parser streams. Do not log paths,
  bucket/key pairs, state JSON, or parsed secret values.
- Use the workflow claim fencing token from `collector.ClaimedService`; do not
  create a second claim lifecycle in this command.
- S3 access must stay read-only and must go through `terraformstate.S3ObjectClient`.
- Redaction key material is required at startup and must never be hardcoded.
- The versioned `redact.RuleSet` is required at startup. `service.go` MUST set
  `tfstateruntime.ClaimedSource.RedactionRules` from
  `config.RedactionRules`; blank `RedactionRules.version` makes the redactor
  fail closed and silently breaks attribute-level drift detection. See
  `service_test.go:TestBuildClaimedServiceWiresRedactionRules` for the
  regression guard.

## Common Changes

- Run `scripts/verify-package-docs.sh` whenever the change adds or edits a Go
  package under this command or `go/internal/collector/terraformstate`.
- Run `scripts/verify-performance-evidence.sh` whenever the change touches
  claims, leases, worker fanout, batching, parser volume, S3 pagination, queue
  pressure, or downstream graph/materialization cost. The PR must include
  tracked Performance Evidence and Observability Evidence markers or
  the corresponding no-regression/no-observability-change markers.

## Anti-patterns

- Running Terraform-state collection inside `workflow-coordinator`.
- Guessing local `.tfstate` files from Git content. Until the #140 approval
  path exists, repo-local state must be configured as an explicit absolute
  source.
- Opening S3 prefixes, workspaces, or non-exact object keys.
- Swallowing claim errors or retrying fenced mutations locally.
- Printing raw state locators or secret material in startup errors.
