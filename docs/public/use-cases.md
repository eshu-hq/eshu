# Use Cases

Eshu is useful when the answer crosses repositories, dependencies, supply chain,
deployment config, runtime topology, and documentation. Each area below names
the MCP tools that lead the workflow.

## Triage A Vulnerability

The launch beachhead. Ask:

- "Which deployed images and workloads are affected by this advisory?"
- "Is the vulnerable symbol actually reachable, or only present?"
- "What is the priority once KEV, EPSS, OSV, and NVD are reconciled?"

Start with `list_supply_chain_impact_findings` to see affected entities, then
`explain_supply_chain_impact` for the evidence chain and `list_advisory_evidence`
for the underlying advisory records. Reachability and suppression are reflected
in the findings so a present-but-unreachable dependency is not over-reported.

## Audit Secrets And IAM

Ask:

- "Where are hardcoded secrets in this code?"
- "What secrets can this workload or principal reach, and through which path?"
- "Which identities trust each other or can escalate privilege?"

Use `investigate_hardcoded_secrets` for in-code findings, then
`list_secrets_iam_secret_access_paths` and
`list_secrets_iam_identity_trust_chains` for the access and trust graph.

## Verify Image Provenance

Ask:

- "Which SBOM attestations are attached to this image, by subject digest?"
- "Do our security-alert findings reconcile with the indexed evidence?"

Use `list_sbom_attestation_attachments` for attachment evidence and
`list_security_alert_reconciliations` to compare alert findings with indexed
truth.

## Before You Merge

Ask:

- "What breaks if I change this?"
- "What is the direct and transitive impact?"
- "Which repos, files, workloads, and resources are involved?"

Through MCP, start with `analyze_pre_change_impact` when you have a diff or
changed-file list; use `investigate_change_surface` when you already know the
entity, service, or topic. Through the CLI, use pre-change impact for a local
diff or bounded graph neighborhood for an entity:

```bash
eshu change impact --repo-id git-repository:payments --base origin/main --head HEAD
eshu map --from payments-api --type service --env prod
```

## During An Incident

Ask:

- "Which workloads use this resource?"
- "What provisions it?"
- "Which repository and files should I inspect first?"
- "How is the affected service deployed?"

Use `investigate_resource` when you start from a resource. Use
`trace_deployment_chain` or `eshu trace service` when you start from a service:

```bash
eshu trace service payments-api --env prod
```

Eshu should return limitations when evidence is missing instead of inventing a
controller, runtime platform, or environment.

## Onboarding

Ask:

- "Explain this service to a new engineer."
- "Scan related repositories, deployment sources, and indexed documentation."
- "Show the source, docs, manifest, and deployment evidence behind the summary."

Use `get_service_story` for the normal dossier path. Use `investigate_service`
when you want coverage first, then `build_evidence_citation_packet` for exact
proof.

## Comparing Environments

Ask:

- "Compare prod and staging for this workload."
- "What resources are shared versus dedicated?"
- "Which config, deployment, or runtime evidence differs?"

Use `compare_environments` through MCP or the HTTP API. Include the workload and
both environment names.

## Reclaim And Re-platform Infrastructure

Ask:

- "Which cloud resources are not managed by any indexed IaC?"
- "How would I bring this resource under Terraform?"
- "What is the readiness plan to re-platform this workload to another cloud?"

Use `find_unmanaged_resources` to find drift, `propose_terraform_import_plan` to
generate an import path, and `compose_replatforming_plan` for a multi-cloud
re-platforming plan.

## Map Dependencies

Ask:

- "What does this package depend on across ecosystems?"
- "Which repositories and images pull in this dependency?"

Use `list_package_registry_dependencies` to walk the dependency set across the
indexed package ecosystems.

## Read Next

- [Use Eshu](use/index.md)
- [Index Repositories](use/index-repositories.md)
- [Starter Prompts](guides/starter-prompts.md)
- [MCP Guide](guides/mcp-guide.md)
