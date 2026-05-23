# Fixture Ecosystems

Fixture ecosystems are small repository layouts under
`tests/fixtures/ecosystems/`. They let tests exercise parsers, indexing, graph
projection, and cross-repo relationship inference without depending on a private
corpus.

## What They Cover

The fixture tree includes:

- language-comprehensive repos, one per parser family
- IaC-comprehensive repos for Terraform, Terragrunt, Helm, Kubernetes,
  Kustomize, ArgoCD, Crossplane, and CloudFormation
- code-plus-IaC combinations such as Python with Terraform or Crossplane
- replay fixtures for analytics, governance, quality, semantic, warehouse, and
  BI paths
- full-platform layouts such as Ansible/Jenkins automation, Helm/ArgoCD, and
  shared infrastructure

Use [Parser Feature Matrix](../languages/feature-matrix.md) and
[Parser Support Matrix](../languages/support-maturity.md) for the current
language inventory instead of duplicating the fixture list here.

## Use Fixtures Locally

Compose mounts `tests/fixtures/ecosystems/` as `/fixtures` by default:

```bash
docker compose up --build
```

To index one subset:

```bash
ESHU_FILESYSTEM_HOST_ROOT=./tests/fixtures/ecosystems/python_terraform \
  docker compose up --build
```

After indexing, query through the HTTP API or MCP.

## Use Fixtures In Tests

```bash
cd go
go test ./internal/parser ./internal/collector ./internal/relationships -count=1
```

Add a new ecosystem only when it proves a real parser, collector, relationship,
or query contract. Keep fixture READMEs focused on what the fixture proves and
which files carry the evidence.

## Related Docs

- [Local Testing](../reference/local-testing.md)
- [Relationship Mapping](../reference/relationship-mapping.md)
- [MCP Guide](mcp-guide.md)
