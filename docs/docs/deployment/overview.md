# Deployment overview

The deployment docs now split local proof paths from the Kubernetes path.

| Path | Use it when | Start here |
| --- | --- | --- |
| Docker Compose | You want the full stack on a laptop. | [Docker Compose](docker-compose.md) |
| Kubernetes | You are deploying Eshu for a team. | [Deploy to Kubernetes](../deploy/kubernetes/index.md) |
| Service runtimes | You need the operator model for each process. | [Service Runtimes](service-runtimes.md) |

The Kubernetes lane covers storage, Helm, raw manifests, Argo CD, production
checks, upgrades, and rollbacks. It is the right path for platform and DevOps
engineers.
