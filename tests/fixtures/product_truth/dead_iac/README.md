# Dead-IaC Product Truth Fixture

This generic corpus proves IaC reachability and dead-IaC findings for API, MCP,
reducer, and storage tests.

The expected truth file owns the assertions:
`tests/fixtures/product_truth/expected/dead_iac.json`.

| Family | Required cases |
| --- | --- |
| Terraform | used, unused, and dynamically sourced modules. |
| Helm | charts reached by ArgoCD or workflow commands, unused charts, and dynamically templated charts. |
| Kustomize | bases and overlays reached by ArgoCD or kustomization resources, unused bases, and dynamically selected targets. |
| Ansible | roles/playbooks reached by controllers, unused roles/playbooks, and dynamically selected roles. |
| Docker Compose | services reached by workflow commands, unused services, and dynamically selected services. |

Dynamic cases are ambiguous by design and must not become confidently dead
without renderer or runtime evidence.
