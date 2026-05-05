## Correlation DSL Generic Corpus

This fixture corpus is a generic multi-repository ecosystem for correlation DSL
and compose-backed verification work.

Each top-level directory is treated as one repository by the filesystem source
mode and the exact repository rules passed by
`scripts/verify_correlation_dsl_compose.sh`.

Repositories:

- `service-gha`
  - service repo with a workload `Dockerfile`
  - GitHub Actions workflow that checks out `deploy-repo`
  - local Kubernetes manifests under `deploy/kubernetes`
- `service-jenkins`
  - service repo with a workload `Dockerfile`
  - `Jenkinsfile` that references `terraform-stack-jenkins`
- `service-jenkins-ansible`
  - Jenkins-driven service repo with an Ansible deployment handoff
  - `Jenkinsfile` invokes `ansible-playbook playbooks/deploy.yml`
  - inventory, var, and role task files keep the Ansible family detectable
- `service-compose`
  - service repo with `docker-compose.yaml`
  - runtime signals include build context, ports, environment, and `depends_on`
- `deploy-repo`
  - admitted service path at `argocd/service-gha/base/application.yaml`
  - unrelated shared config at `argocd/shared-config/base/configmap.yaml`
  - the shared config path should remain provenance-only for unrelated services
- `terraform-stack-gha`
  - Terraform stack with explicit `service-gha` signals
- `terraform-stack-jenkins`
  - Terraform stack with explicit `service-jenkins` signals
- `multi-dockerfile-repo`
  - one workload `Dockerfile`
  - one utility-only `Dockerfile.test`
  - local Kubernetes manifest for the workload image only

The current compose verification lane proves:

- repository selection stays exact and explicit
- GitHub Actions, Jenkins, Jenkins plus Ansible, Docker Compose, Dockerfile,
  ArgoCD, and Terraform artifacts are present in the indexed corpus
- the corpus contains both likely-admission and likely-rejection cases

Future reducer assertions should extend the compose lane to verify canonical
admission, rejected utility images, and provenance-only shared-config behavior
directly from materialized correlation outputs.
