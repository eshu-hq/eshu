<!-- docs-catalog
title: Trace Infrastructure
description: Shows how to trace services, workloads, cloud resources, Terraform, and deployment evidence.
type: how-to
audience: platform-engineer, operator
entrypoint: true
landing: false
-->

# Trace infrastructure

Use this path when you need to understand what deploys a service, what
resources it uses, or what might break when infrastructure changes.

## Choose a starting point

Start with one of these:

- service or workload name
- Kubernetes object
- Argo CD application
- Terraform module or resource
- Helm chart
- repository name
- environment name

## Trace from the CLI

```bash
eshu trace service payments-api --env prod
eshu map --from payments-api --type service --env prod
```

`eshu trace service` renders the service story from the API. `eshu map` renders
a bounded graph neighborhood from one resolved entity.

## Trace from an MCP client

- "What deploys this service to prod?"
- "Which workloads use this database?"
- "Trace this RDS instance back to Terraform."
- "Compare stage and prod for this workload."
- "What changes if this Helm chart changes?"

Ask for evidence for each hop:

> Use Eshu to trace this workload to deployment sources and backing
> infrastructure. Show the repos, files, and graph relationships that support
> each step.

## Verify each hop

Confirm that every step names its environment, source repository or file, and
graph relationship. Narrow a mixed-environment result with `--env`, then rerun
the trace before acting on it.

## Read next

- [Starter Prompts](../guides/starter-prompts.md)
- [Relationship Graph Examples](../guides/relationship-graphs.md)
- [HTTP API](../reference/http-api.md)
