# Environment Variables: Infrastructure and registry collectors

<!-- Generated from go/internal/envregistry. Do not edit by hand; regenerate with `bash scripts/generate-env-registry-doc.sh`. -->

Cloud, Kubernetes, OCI registry, and package registry collectors. AWS cloud and Terraform state now collect from their extracted satellites (eshu-hq/eshu-collector-aws-cloud, eshu-hq/eshu-collector-terraform-state). Vault now collects from its extracted satellite (eshu-hq/eshu-collector-vault-live). Azure now collects from its extracted satellite (eshu-hq/eshu-collector-azure-cloud). This page is part of the [Environment Variable Reference](../env-registry.md), which lists every page. Run `eshu config validate` to check the current environment against the registry.

## collector-gcp-cloud

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_GCP_COLLECTOR_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_GCP_COLLECTOR_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_GCP_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_GCP_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_GCP_COLLECTOR_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |
| `ESHU_GCP_COLLECTOR_QUOTA_PROJECT_ID` | string | — | Optional billing/quota project id sent as x-goog-user-project on Cloud Asset Inventory requests. Leave unset for service-account/Workload Identity Federation ADC; set it when the resolved ADC is a user credential, which otherwise gets a 403 quota-project error. |

## collector-kubernetes-live

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_KUBERNETES_LIVE_CLUSTERS_JSON` | string | — | JSON array of Kubernetes cluster targets with auth configuration. |
| `ESHU_KUBERNETES_LIVE_COLLECTOR_INSTANCE_ID` | string | — | Instance ID for the kubernetes-live collector. |
| `ESHU_KUBERNETES_LIVE_POLL_INTERVAL` | duration | `5m` | Poll interval for discovering Kubernetes resources. |

## collector-oci-registry

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_OCI_REGISTRY_ALLOW_EMPTY_SUCCESSOR` | bool | `false` | Collector-wide trusted-deletion attestation: when true, every OCI registry generation carries TrustedDeletion, bypassing the ingestion store's successor-emptiness backstop for legitimately emptied repositories. Prefer the per-target allow_empty_successor key when only some repositories may legitimately empty. |
| `ESHU_OCI_REGISTRY_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_OCI_REGISTRY_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_OCI_REGISTRY_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_OCI_REGISTRY_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_OCI_REGISTRY_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |
| `ESHU_OCI_REGISTRY_TARGETS_JSON` | string | — | JSON array of OCI registry target configurations. |

## collector-package-registry

| Variable | Type | Default | Notes |
| --- | --- | --- | --- |
| `ESHU_PACKAGE_REGISTRY_CLAIM_LEASE_TTL` | duration | `60s` | Workflow claim lease TTL. |
| `ESHU_PACKAGE_REGISTRY_COLLECTOR_INSTANCE_ID` | string | — | Instance ID selecting this collector instance from ESHU_COLLECTOR_INSTANCES_JSON. |
| `ESHU_PACKAGE_REGISTRY_COLLECTOR_OWNER_ID` | string | — | Lease owner identifier; defaults to the hostname. |
| `ESHU_PACKAGE_REGISTRY_HEARTBEAT_INTERVAL` | duration | `20s` | Claim heartbeat interval; must be less than the lease TTL. |
| `ESHU_PACKAGE_REGISTRY_POLL_INTERVAL` | duration | `1s` | Poll interval for discovering targets. |
