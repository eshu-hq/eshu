# Templated IaC Fixture Corpus

Sanitized from local real-world source files under `~/repos`.
These fixtures preserve templating structure while removing company domains, org names, registries, and secrets.

Regeneration note:

```bash
# The old local-only generator is no longer shipped in this repository.
# Update these fixtures manually from sanitized source examples and keep this
# README in sync with the corpus layout.
```

## ansible_jinja

- `example-ansible-templates/roles/builder/templates/Dockerfile.j2`
  - Source: `~/repos/ansible-automate/automate-build-golden-ami/roles/build-ami/templates/Dockerfile.j2`
  - Notes: replace private artifact registry
- `example-ansible-templates/roles/web/templates/site.conf.j2`
  - Source: `~/repos/ansible-automate/automate-mws/roles/portal-websites/templates/portal-dmmwebsites.conf.j2`
  - Notes: replace company domains and custom header names

## dagster_jinja_yaml

- `example-dagster-assets/Dockerfile`
  - Source: `~/repos/services/bg-dagster/Dockerfile`
  - Notes: preserve a plain Dockerfile for raw-text ingestion
- `example-dagster-assets/assets/data_lakehouse/branch_ingestion.yaml`
  - Source: `~/repos/services/bg-dagster/bg_data_platform/assets/data_lakehouse/branchio_ingestion.yaml`
  - Notes: preserve Jinja loops over YAML structures
- `example-dagster-assets/assets/data_quality/analytics_checks.yaml`
  - Source: `~/repos/services/bg-dagster/bg_data_platform/assets/data_quality/ga4_dq_checks.yaml`
  - Notes: replace portal names and scrub webhook

## helm_go_template

- `example-platform-chart/chart/Chart.yaml`
  - Source: `~/repos/mobius/iac-eks-eshu/chart/Chart.yaml`
  - Notes: genericize chart identity
- `example-platform-chart/chart/templates/_helpers.tpl`
  - Source: `~/repos/mobius/iac-eks-eshu/chart/templates/_helpers.tpl`
  - Notes: preserve Go-template helpers
- `example-platform-chart/chart/templates/deployment.yaml`
  - Source: `~/repos/mobius/iac-eks-eshu/chart/templates/deployment.yaml`
  - Notes: preserve Go-template YAML control flow
- `example-platform-chart/chart/values.yaml`
  - Source: `~/repos/mobius/iac-eks-eshu/chart/values.yaml`
  - Notes: replace registry, org, and secret names

## terraform_template_text

- `example-terraform-templates/templates/ecs/container.tpl`
  - Source: `~/repos/terraform-modules/terraform-snapshots/modules/ecs/application-cloudmap/templates/ecs/container.tpl`
  - Notes: preserve Terraform interpolation placeholders
