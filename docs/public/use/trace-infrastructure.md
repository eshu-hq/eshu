<!-- docs-catalog
title: Trace Infrastructure
description: Shows how to trace services, workloads, cloud resources, Terraform, and deployment evidence.
type: how-to
audience: platform-engineer, operator
entrypoint: true
landing: false
-->

# Trace Infrastructure

Use this path when you need to understand what deploys a service, what
resources it uses, or what might break when infrastructure changes.

## Good Starting Points

Start with one of these:

- service or workload name
- Kubernetes object
- Argo CD application
- Terraform module or resource
- Helm chart
- repository name
- environment name

## CLI Examples

```bash
eshu trace service payments-api --env prod
eshu map --from payments-api --type service --env prod
```

`eshu trace service` renders the service story from the API. `eshu map` renders
a bounded graph neighborhood from one resolved entity.

## MCP Examples

- "What deploys this service to prod?"
- "Which workloads use this database?"
- "Trace this RDS instance back to Terraform."
- "Compare stage and prod for this workload."
- "What changes if this Helm chart changes?"

Ask for evidence for each hop:

> Use Eshu to trace this workload to deployment sources and backing
> infrastructure. Show the repos, files, and graph relationships that support
> each step.

## Read Next

- [Starter Prompts](../guides/starter-prompts.md)
- [Relationship Graph Examples](../guides/relationship-graphs.md)
- [HTTP API](../reference/http-api.md)
