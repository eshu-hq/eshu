# Fixture Ecosystems

Integration tests need realistic multi-repo data — not toy examples with one file and a `main()`. Fixture ecosystems are pre-built repository layouts that simulate real platform topologies: services with IaC, Helm charts with ArgoCD apps, shared infrastructure consumed by multiple workloads.

## What's in a fixture ecosystem

Each ecosystem is a directory under `tests/fixtures/ecosystems/` containing the file structure of one or more simulated repositories. They are realistic enough to exercise Eshu's parsers, graph construction, and cross-repo relationship inference — and small enough to index in seconds.

## Available ecosystems

Eshu ships 32 fixture ecosystems organized into four categories:

**Language-comprehensive** — full parser coverage for a single language:

`c_comprehensive`, `cpp_comprehensive`, `csharp_comprehensive`, `dart_comprehensive`, `elixir_comprehensive`, `go_comprehensive`, `haskell_comprehensive`, `java_comprehensive`, `javascript_comprehensive`, `kotlin_comprehensive`, `perl_comprehensive`, `php_comprehensive`, `python_comprehensive`, `ruby_comprehensive`, `rust_comprehensive`, `scala_comprehensive`, `swift_comprehensive`, `tsx_comprehensive`, `typescript_comprehensive`

**IaC-comprehensive** — deep coverage for a single IaC format:

`terraform_comprehensive`, `terragrunt_comprehensive`, `helm_comprehensive`, `kubernetes_comprehensive`, `kustomize_comprehensive`, `argocd_comprehensive`, `crossplane_comprehensive`, `cloudformation_comprehensive`

**Code + IaC combos** — a service repo paired with its infrastructure:

`code_only_python`, `python_terraform`, `python_crossplane`

**Full platform** — multi-repo layouts with deployment topology:

`helm_argocd_platform`, `shared_infra_platform`

## Using fixtures with docker-compose

The `docker-compose.yaml` at the project root mounts `tests/fixtures/ecosystems/` as the fixture source by default. Start the stack and Eshu indexes every ecosystem:

```bash
docker compose up -d
```

To index a specific ecosystem subset, set `ESHU_FILESYSTEM_HOST_ROOT`:

```bash
ESHU_FILESYSTEM_HOST_ROOT=./tests/fixtures/ecosystems/python_terraform \
  docker compose up -d
```

Once indexed, query the graph through the HTTP API or MCP:

```bash
# List what got indexed
curl -s http://localhost:8080/api/v0/repositories | jq '.[].name'

# Query via MCP
eshu mcp start
```

## Using fixtures in tests

Fixture ecosystems back the Go test suite. Tests index a fixture, then query
the resulting graph to verify parser correctness and relationship inference:

```bash
# Run Go parser and collector tests that exercise fixture ecosystems
cd go && go test ./internal/parser ./internal/collector ./internal/relationships -count=1
```

## Adding a new ecosystem

1. Create a directory under `tests/fixtures/ecosystems/` — name it `{language}_{iac}` or `{format}_comprehensive`
2. Add realistic files: source code, IaC definitions, manifests, `README.md`
3. Include cross-references if testing relationship inference (e.g., a Terraform module referencing a repo URL)
4. Run `eshu index tests/fixtures/ecosystems/your_ecosystem` to verify parsing
5. Add integration tests that assert expected graph structure

## Next steps

- [How It Works](../concepts/how-it-works.md) — understand the indexing pipeline these fixtures exercise
- [CI/CD Integration](ci-cd.md) — run fixture-backed checks in your pipeline
- [MCP Guide](mcp-guide.md) — query fixture data through your AI assistant
