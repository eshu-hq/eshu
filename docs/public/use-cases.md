# Use Cases

Eshu is useful when the answer crosses repositories, deployment config, runtime
topology, and documentation.

## Before You Merge

Ask:

- "What breaks if I change this?"
- "What is the direct and transitive impact?"
- "Which repos, files, workloads, and resources are involved?"

Through MCP, start with `investigate_change_surface`. Through the CLI, use a
bounded graph neighborhood:

```bash
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

## Read Next

- [Use Eshu](use/index.md)
- [Index Repositories](use/index-repositories.md)
- [Starter Prompts](guides/starter-prompts.md)
- [MCP Guide](guides/mcp-guide.md)
