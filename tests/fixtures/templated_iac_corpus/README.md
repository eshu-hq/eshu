# Templated IaC Fixture Corpus

This directory is parser and collector test input for templated
infrastructure-as-code files. The examples are sanitized fixtures: they keep
template syntax, control flow, and infrastructure-shaped content without
preserving source-project paths, private domains, registries, account IDs, or
secrets.

## What This Proves

- Jinja Dockerfile and Nginx config templates keep delimiters and conditional
  blocks visible to parsers.
- Jinja YAML assets preserve loop and variable syntax inside YAML documents.
- Helm chart files preserve Go-template helpers, conditionals, values, and
  Kubernetes resource shape.
- Terraform text templates preserve `${...}` interpolation inside container
  definition JSON.

## Layout

| Path | Fixture role |
| --- | --- |
| `example-ansible-templates/roles/builder/templates/Dockerfile.j2` | Jinja-templated Dockerfile input. |
| `example-ansible-templates/roles/web/templates/site.conf.j2` | Jinja-templated Nginx service config. |
| `example-dagster-assets/Dockerfile` | Plain Dockerfile control input. |
| `example-dagster-assets/assets/data_lakehouse/branch_ingestion.yaml` | YAML with Jinja loop and variable expressions. |
| `example-dagster-assets/assets/data_quality/analytics_checks.yaml` | YAML data-quality fixture with scrubbed webhook-style content. |
| `example-platform-chart/chart/Chart.yaml` | Helm chart identity fixture. |
| `example-platform-chart/chart/templates/_helpers.tpl` | Helm helper-template fixture. |
| `example-platform-chart/chart/templates/deployment.yaml` | Helm deployment template with values, helpers, and control flow. |
| `example-platform-chart/chart/values.yaml` | Helm values fixture with sanitized registry and secret names. |
| `example-terraform-templates/templates/ecs/container.tpl` | Terraform template interpolation inside ECS container JSON. |
| `manifest.json` | Corpus metadata for fixture families and target paths. |

## Update Rules

- Keep examples sanitized. Do not add real domains, registries, org names,
  account IDs, hostnames, or secrets.
- Preserve the template-language signal instead of replacing files with rendered
  output.
- Update `manifest.json` and this README together when files are added,
  renamed, or removed.
- Do not add setup or run instructions; this directory is fixture input, not an
  application.

## Assertion Surface

Use this corpus when changing parser or collector behavior around templated
infrastructure files. Keep code-level assertions in the parser, collector, or
query tests that consume these files; keep this README limited to fixture
intent, layout, and maintenance rules.
