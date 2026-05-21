# Helm collector and webhook values

Use this page to enable optional collectors and public webhook intake in the
Helm chart. The chart source is `deploy/helm/eshu`.

## Collector modes

The public chart supports two collector styles.

| Style | Workloads | Work source |
| --- | --- | --- |
| Direct-target collector | Confluence, OCI registry | Target values under the collector block. |
| Claim-driven collector | Terraform-state, AWS cloud, Package Registry | Durable claims created by the workflow coordinator. |

Claim-driven collectors require the workflow coordinator in active claim mode.
The chart rejects installs where Terraform-state, AWS cloud, or Package
Registry collector deployments are enabled without
`workflowCoordinator.enabled=true`, `workflowCoordinator.deploymentMode=active`,
and `workflowCoordinator.claimsEnabled=true`.

## Workflow coordinator

Enable the coordinator before enabling claim-driven collectors.

```yaml
workflowCoordinator:
  enabled: true
  deploymentMode: active
  claimsEnabled: true
  collectorInstances:
    - instance_id: package-registry-primary
      collector_kind: package_registry
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: npm
            ecosystem: npm
            registry: https://registry.npmjs.org
            scope_id: npm://registry.npmjs.org/lodash
            packages: [lodash]
            package_limit: 1
            version_limit: 2
            metadata_url: https://registry.npmjs.org/lodash
```

## Confluence collector

The Confluence collector is off by default. When enabled, it stores
documentation sections in Postgres and keeps the runtime read-only against
Confluence.

| Value | Default | Purpose |
| --- | --- | --- |
| `confluenceCollector.enabled` | `false` | Deploy the collector. |
| `confluenceCollector.baseUrl` | empty | Atlassian wiki base URL. |
| `confluenceCollector.spaceId` | empty | Crawl one Confluence space by ID. |
| `confluenceCollector.spaceIds` | `[]` | Crawl an explicit allowlist of Confluence space IDs. |
| `confluenceCollector.spaceKey` | empty | Optional space key rendered to `ESHU_CONFLUENCE_SPACE_KEY`. |
| `confluenceCollector.rootPageId` | empty | Crawl under one root page. |
| `confluenceCollector.pageLimit` | empty | Optional page limit. |
| `confluenceCollector.pollInterval` | `5m` | Poll interval. |
| `confluenceCollector.credentials.secretName` | empty | Secret containing Confluence auth material. |

The chart requires `baseUrl`, credentials, and exactly one crawl scope:
`spaceId`, `spaceIds`, or `rootPageId`.

Use email/API-token credentials:

```yaml
confluenceCollector:
  enabled: true
  baseUrl: https://example.atlassian.net/wiki
  spaceId: "123456789"
  credentials:
    secretName: confluence-collector-credentials
    emailKey: email
    apiTokenKey: api-token
```

Or use a bearer token and a multi-space allowlist:

```yaml
confluenceCollector:
  enabled: true
  baseUrl: https://example.atlassian.net/wiki
  spaceIds:
    - "123456789"
    - "987654321"
  credentials:
    secretName: confluence-collector-credentials
    bearerTokenKey: token
```

## OCI registry collector

The OCI registry collector is off by default. When enabled, it scans configured
registry repositories and writes digest-addressed image facts to Postgres.

| Value | Default | Purpose |
| --- | --- | --- |
| `ociRegistryCollector.enabled` | `false` | Deploy the collector. |
| `ociRegistryCollector.instanceId` | `oci-registry-primary` | Instance ID used in emitted scope metadata. |
| `ociRegistryCollector.pollInterval` | `5m` | Poll interval. |
| `ociRegistryCollector.targets` | `[]` | Direct registry targets. Required when enabled. |
| `ociRegistryCollector.aws.region` | empty | Optional default AWS region for ECR targets. |
| `ociRegistryCollector.extraEnv` | `[]` | Secret-backed env vars used by target credential indirection. |

The chart schema allows `jfrog`, `ecr`, `dockerhub`, and `ghcr` target
providers. This chart keeps OCI registry in explicit direct-target mode through
`ociRegistryCollector.targets`. Claim-aware OCI mode remains a runtime feature
for local or custom deployments that set `ESHU_COLLECTOR_INSTANCES_JSON`
directly.

ECR on EKS should use IAM Roles for Service Accounts through
`serviceAccount.annotations`; do not set `aws_profile` in Kubernetes values.

```yaml
serviceAccount:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-oci-registry-collector

ociRegistryCollector:
  enabled: true
  instanceId: oci-registry-primary
  aws:
    region: us-east-1
  targets:
    - provider: ecr
      registry_id: "123456789012"
      region: us-east-1
      repository: team/api
      references: ["latest"]
    - provider: dockerhub
      repository: library/busybox
      references: ["latest"]
```

Private JFrog, Docker Hub, and GHCR targets should use target-level env
indirection plus `extraEnv` Secret refs:

```yaml
ociRegistryCollector:
  enabled: true
  targets:
    - provider: jfrog
      base_url: https://artifacts.example.test
      repository_key: docker-local
      repository: team/app
      username_env: JFROG_USERNAME
      password_env: JFROG_PASSWORD
  extraEnv:
    - name: JFROG_USERNAME
      valueFrom:
        secretKeyRef:
          name: jfrog-oci-credentials
          key: username
    - name: JFROG_PASSWORD
      valueFrom:
        secretKeyRef:
          name: jfrog-oci-credentials
          key: password
```

This collector currently proves registry-to-Postgres fact ingestion. Graph
projection and API/MCP image-correlation answers are a separate promotion step.

## Terraform-state collector

Terraform-state is claim-driven and uses secret-backed redaction. Do not put
redaction keys or state credentials in values.

| Value | Default | Purpose |
| --- | --- | --- |
| `terraformStateCollector.enabled` | `false` | Deploy the collector. |
| `terraformStateCollector.instanceId` | `terraform-state-primary` | Selected instance ID. |
| `terraformStateCollector.pollInterval` | `1s` | Claim polling interval. |
| `terraformStateCollector.claimLeaseTTL` | empty | Optional claim lease TTL override. |
| `terraformStateCollector.heartbeatInterval` | empty | Optional claim heartbeat interval override. |
| `terraformStateCollector.sourceMaxBytes` | empty | Optional maximum source payload size. |
| `terraformStateCollector.collectorInstances` | `[]` | Rendered to `ESHU_COLLECTOR_INSTANCES_JSON`. Required when enabled. |
| `terraformStateCollector.redaction.secretName` | empty | Secret containing the redaction key. Required when enabled. |
| `terraformStateCollector.redaction.keyKey` | `redaction-key` | Secret key name for the redaction key. |
| `terraformStateCollector.redaction.rulesetVersion` | empty | Redaction ruleset version. Required when enabled. |

```yaml
terraformStateCollector:
  enabled: true
  instanceId: terraform-state-primary
  redaction:
    secretName: tfstate-redaction
    keyKey: redaction-key
    rulesetVersion: schema-v1
  collectorInstances:
    - instance_id: terraform-state-primary
      collector_kind: terraform_state
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        target_scopes:
          - target_scope_id: aws-prod
            provider: aws
            deployment_mode: central
            credential_mode: local_workload_identity
            allowed_regions: [us-east-1]
            allowed_backends: [s3]
```

## AWS cloud collector

AWS cloud is claim-driven. On EKS, use workload identity instead of static
access keys.

```yaml
awsCloudCollector:
  enabled: true
  instanceId: aws-primary
  serviceAccount:
    create: true
    annotations:
      eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/eshu-aws-collector
  collectorInstances:
    - instance_id: aws-primary
      collector_kind: aws
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        target_scopes:
          - account_id: "123456789012"
            allowed_regions: [us-east-1]
            allowed_services: [iam, ecr]
            credentials:
              mode: local_workload_identity
```

Use `awsCloudCollector.redaction.secretName` only when the selected AWS scan
requires redaction material. The chart renders a dedicated ServiceAccount when
`awsCloudCollector.serviceAccount.create=true`; otherwise it uses the shared
release ServiceAccount.

## Package Registry collector

Package Registry is claim-driven. Credentials use target-level env indirection
plus Secret refs.

```yaml
packageRegistryCollector:
  enabled: true
  collectorInstances:
    - instance_id: package-registry-primary
      collector_kind: package_registry
      mode: continuous
      enabled: true
      claims_enabled: true
      configuration:
        targets:
          - provider: jfrog
            ecosystem: npm
            registry: https://artifacts.example.test
            scope_id: npm://artifacts.example.test/team/app
            metadata_url: https://artifacts.example.test/api/npm/team/app
            username_env: PACKAGE_REGISTRY_USERNAME
            password_env: PACKAGE_REGISTRY_PASSWORD
  extraEnv:
    - name: PACKAGE_REGISTRY_USERNAME
      valueFrom:
        secretKeyRef:
          name: package-registry-credentials
          key: username
    - name: PACKAGE_REGISTRY_PASSWORD
      valueFrom:
        secretKeyRef:
          name: package-registry-credentials
          key: password
```

## Webhook listener

The webhook listener is off by default. When enabled, it accepts provider
webhook deliveries, verifies provider secrets, and writes refresh triggers to
Postgres. It does not mount the repository workspace PVC or graph credentials.

| Value | Default | Purpose |
| --- | --- | --- |
| `webhookListener.enabled` | `false` | Deploy the listener. |
| `webhookListener.maxBodyBytes` | `1048576` | Maximum webhook body size. |
| `webhookListener.defaultBranch` | empty | Optional default branch for refresh triggers. |
| `webhookListener.github.enabled` | `false` | Enable the GitHub route. |
| `webhookListener.gitlab.enabled` | `false` | Enable the GitLab route. |
| `webhookListener.bitbucket.enabled` | `false` | Enable the Bitbucket route. |
| `webhookListener.awsFreshness.enabled` | `false` | Enable the AWS freshness route. |
| `webhookListener.exposure.ingress.enabled` | `false` | Render provider-only ingress paths. |

The chart requires at least one provider route when the listener is enabled.
Each enabled provider requires its Secret name.

```yaml
webhookListener:
  enabled: true
  github:
    enabled: true
    secretName: github-webhook-secret
  bitbucket:
    enabled: true
    secretName: bitbucket-webhook-secret
  awsFreshness:
    enabled: true
    secretName: aws-freshness-webhook-token
  exposure:
    ingress:
      enabled: true
      hosts:
        - host: hooks.example.com
```

Only provider webhook paths are routed by the chart ingress. Set those paths
with `webhookListener.github.path`, `webhookListener.gitlab.path`,
`webhookListener.bitbucket.path`, and `webhookListener.awsFreshness.path`.
Runtime health, status, and metrics endpoints stay on the internal service
unless an operator adds separate protected routing.
