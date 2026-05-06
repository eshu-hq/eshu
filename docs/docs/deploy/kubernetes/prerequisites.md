# Kubernetes prerequisites

Before installing Eshu, prepare the cluster and external services.

## Tools

You need:

- `kubectl` access to the target cluster
- Helm 3
- a namespace for Eshu, usually `eshu`
- access to create `Secret`, `Deployment`, `StatefulSet`, `Service`, and PVC
  resources
- Prometheus Operator only if you enable `ServiceMonitor`
- Gateway API CRDs only if you use `exposure.gateway`

## Storage services

Eshu expects external storage:

- Postgres for facts, queues, status, content, and recovery data
- NornicDB by default for the canonical graph
- Neo4j only when you set `env.ESHU_GRAPH_BACKEND=neo4j`

The chart only creates the ingester workspace PVC. It does not create database
instances.

## Secrets

| Secret | Default value path | Keys |
| --- | --- | --- |
| API bearer token | `apiAuth.secretName=eshu-api-auth` | `api-key` |
| Graph auth | `neo4j.auth.secretName=eshu-neo4j` | `username`, `password` |
| GitHub App auth | `repoSync.auth.githubApp.secretName=github-app-credentials` | `app-id`, `installation-id`, `private-key` |
| Git token auth | `repoSync.auth.token.secretName=github-token` | `token` |
| Git SSH auth | `repoSync.auth.ssh.secretName=github-ssh` | `id_rsa`, `known_hosts` |

Only one repository auth method is used at a time. The default is
`repoSync.auth.method=githubApp`.

For bundled NornicDB no-auth installs, set `neo4j.auth.secretName=""`. The
chart still renders `NEO4J_USERNAME` and `NEO4J_PASSWORD` from
`neo4j.auth.username/password` because Eshu's shared Bolt client config rejects
empty credentials before it opens the connection.
