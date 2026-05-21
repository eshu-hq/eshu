# Use Cases

Eshu is useful when the answer crosses repositories, deployment config,
runtime topology, and documentation.

## Before You Merge

You are changing a service, module, API contract, or deployment setting. Ask:

- "What breaks if I change this?"
- "What is the direct and transitive impact?"
- "Which repos, files, workloads, and resources are involved?"

Through MCP, start with `investigate_change_surface` for changed paths, code
topics, services, modules, or resources. Through the CLI, use `eshu map` for a
bounded graph neighborhood:

```bash
eshu map --from payments-api --type service --env prod
```

## During An Incident

A database, queue, workload, or cloud resource is unhealthy. Ask:

- "Which workloads use this resource?"
- "What provisions it?"
- "Which repository and files should I inspect first?"
- "How is the affected service deployed?"

Use `investigate_resource` when you start from a resource name. Use
`trace_deployment_chain` or `eshu trace service` when you start from a service:

```bash
eshu trace service payments-api --env prod
```

Eshu should return limitations when evidence is missing instead of inventing a
controller, runtime platform, or environment.

## Onboarding A New Engineer

A new engineer needs to understand how a service fits into the platform. Ask:

- "Explain this service to a new engineer."
- "Scan related repositories, deployment sources, and indexed documentation
  before answering."
- "Show the source, docs, manifest, and deployment evidence behind the summary."

Use `get_service_story` for the one-call dossier. Use `investigate_service`
when you want coverage first. Use `build_evidence_citation_packet` after story
or investigation tools return file and entity handles.

## Comparing Environments

Staging is broken but prod works. Ask:

- "Compare prod and staging for this workload."
- "What resources are shared versus dedicated?"
- "Which config, deployment, or runtime evidence differs?"

Use `compare_environments` through MCP or the HTTP API. Include the workload and
both environment names.

## AI-Assisted Workflows

All of these workflows are available through MCP, so an assistant can call Eshu
instead of guessing from one local file.

| Prompt | Start with |
| --- | --- |
| "How is this service deployed?" | `trace_deployment_chain` |
| "Which files influence image tags or limits?" | `investigate_deployment_config` |
| "What provisions this database?" | `investigate_resource` |
| "What breaks if I change this?" | `investigate_change_surface` |
| "Where is this behavior implemented?" | `investigate_code_topic` |
| "Which files or modules import this module?" | `investigate_import_dependencies` |
| "Show source and docs evidence." | `build_evidence_citation_packet` |

## Read Next

- [Run Locally](run-locally/index.md)
- [Use Eshu](use/index.md)
- [Starter Prompts](guides/starter-prompts.md)
- [MCP Guide](guides/mcp-guide.md)
