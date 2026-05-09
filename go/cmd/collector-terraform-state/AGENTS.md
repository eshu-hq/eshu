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

## Anti-patterns

- Running Terraform-state collection inside `workflow-coordinator`.
- Guessing local `.tfstate` files from Git content.
- Opening S3 prefixes, workspaces, or non-exact object keys.
- Swallowing claim errors or retrying fenced mutations locally.
- Printing raw state locators or secret material in startup errors.
