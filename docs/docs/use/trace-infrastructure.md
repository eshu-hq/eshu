# Trace infrastructure

Eshu connects code to deployment and infrastructure evidence. Use this path when
you need to understand what deploys a service, what resources it uses, or what
might break when infrastructure changes.

## Good starting points

Start with one of these:

- workload name
- Kubernetes object
- Argo CD application
- Terraform module or resource
- Helm chart
- repository name
- environment name

## Example questions

- "What deploys this service to prod?"
- "Which workloads use this database?"
- "Trace this RDS instance back to the Terraform module."
- "Compare stage and prod for this workload."
- "What changes if this Helm chart changes?"

For MCP, ask the assistant to use Eshu and include evidence for each hop:

> Use Eshu to trace this workload to deployment sources and backing
> infrastructure. Show the repos, files, and graph relationships that support
> each step.

## Where to go next

- [Starter Prompts](../guides/starter-prompts.md)
- [Relationship Graph Examples](../guides/relationship-graphs.md)
- [HTTP API](../reference/http-api.md)
