# Collector Live Smokes

Use these only for maintainer/operator validation against real external
systems. They are opt-in and must not run in default CI. Keep registry hosts,
account IDs, repository names, usernames, and tokens in local shell config only.

## Confluence Collector Smoke

Use this when testing the Confluence collector against a real Atlassian site.
The collector is read-only against Confluence and writes documentation facts to
the local Postgres content store.

Load your local Jira/Confluence credential file, then normalize the env names
the collector expects:

```bash
set -a
source ~/.jira_api_credentials.conf
set +a

export ESHU_CONFLUENCE_BASE_URL="${CONFLUENCE_BASE_URL:-https://example.atlassian.net/wiki}"
export ESHU_CONFLUENCE_EMAIL="${JIRA_EMAIL:?set JIRA_EMAIL}"
export ESHU_CONFLUENCE_API_TOKEN="${JIRA_API_TOKEN:?set JIRA_API_TOKEN}"
export ESHU_CONFLUENCE_SPACE_KEY="${ESHU_CONFLUENCE_SPACE_KEY:-DEV}"
export ESHU_CONFLUENCE_PAGE_LIMIT="${ESHU_CONFLUENCE_PAGE_LIMIT:-25}"
export ESHU_CONFLUENCE_POLL_INTERVAL="${ESHU_CONFLUENCE_POLL_INTERVAL:-5m}"
```

Resolve the space key to the numeric space ID used by the Confluence API:

```bash
export ESHU_CONFLUENCE_SPACE_ID="$(
  curl -fsS \
    -u "${ESHU_CONFLUENCE_EMAIL}:${ESHU_CONFLUENCE_API_TOKEN}" \
    "${ESHU_CONFLUENCE_BASE_URL}/api/v2/spaces?keys=${ESHU_CONFLUENCE_SPACE_KEY}&limit=1" |
    jq -r '.results[0].id'
)"

test -n "$ESHU_CONFLUENCE_SPACE_ID"
test "$ESHU_CONFLUENCE_SPACE_ID" != "null"
```

Start Postgres, apply the data-plane schema, then run the collector:

```bash
docker compose up -d postgres

cd go
go run ./cmd/bootstrap-data-plane
go run ./cmd/collector-confluence
```

In another shell, check the status endpoint and stored facts:

```bash
curl -fsS http://127.0.0.1:8080/readyz

docker compose exec -T postgres \
  psql postgresql://eshu:change-me@localhost:5432/eshu \
  -c "select fact_kind, count(*) from fact_records where source_system = 'confluence' group by fact_kind order by fact_kind;"
```

Stop the collector with Ctrl-C after the first successful sync unless you are
testing repeated polling.

For a fixture-backed performance and observability proof without live
Atlassian traffic:

```bash
cd go
go test ./internal/collector/confluence \
  -run 'TestSourceRecordsBoundedConfluenceMetrics|TestHTTPClientRecordsBoundedRequestMetrics' \
  -count=1 -v
```

Record the page count, visible document count, emitted section/link counts,
wall time, and HTTP GET count in the changed package README or reference page.

## Vulnerability Intelligence Source Fixture Proof

Use this when changing vulnerability source-client slices. This is not a live
hosted collector smoke. It verifies bounded OSV, CISA KEV, FIRST EPSS, and NVD
request shaping plus source snapshot facts, affected package normalization,
risk-signal facts, fixed-version extraction, and URL credential stripping.

```bash
cd go
go test ./internal/collector/vulnerabilityintelligence -count=1 -v
```

Record expanded source coverage in the changed collector README or
[Collector And Reducer Readiness](../collector-reducer-readiness.md). Hosted
vulnerability-intelligence validation must add request budgets, rate-limit
telemetry, fact-emission metrics, admin/status output, and deployment docs
before enabling hosted collection.

## JFrog OCI Smoke

```bash
set -a
source /path/to/local/private/env
set +a

export ESHU_JFROG_OCI_LIVE=1
export ESHU_JFROG_OCI_URL="${ESHU_JFROG_OCI_URL:-${JFROG_URL:-${JFROG_BASE_URL:-}}}"
export ESHU_JFROG_OCI_REPOSITORY_KEY="${ESHU_JFROG_OCI_REPOSITORY_KEY:-${JFROG_DOCKER_REPOSITORY_KEY:-}}"
export ESHU_JFROG_OCI_IMAGE_REPOSITORY="${ESHU_JFROG_OCI_IMAGE_REPOSITORY:-${JFROG_IMAGE_REPOSITORY:-}}"
export ESHU_JFROG_OCI_REFERENCE="${ESHU_JFROG_OCI_REFERENCE:-${JFROG_IMAGE_REFERENCE:-}}"
export ESHU_JFROG_OCI_USERNAME="${ESHU_JFROG_OCI_USERNAME:-${JFROG_USERNAME:-${JFROG_USER:-}}}"
export ESHU_JFROG_OCI_PASSWORD="${ESHU_JFROG_OCI_PASSWORD:-${JFROG_PASSWORD:-}}"
export ESHU_JFROG_OCI_BEARER_TOKEN="${ESHU_JFROG_OCI_BEARER_TOKEN:-${JFROG_ACCESS_TOKEN:-${JFROG_BEARER_TOKEN:-}}}"

cd go
go test ./internal/collector/ociregistry/jfrog -run TestLiveJFrog -count=1 -v
```

The challenge smoke can run with only `ESHU_JFROG_OCI_URL`. The tag-list smoke
also needs `ESHU_JFROG_OCI_IMAGE_REPOSITORY`.
`ESHU_JFROG_OCI_REPOSITORY_KEY` is required only for the Artifactory
`/artifactory/api/docker/<repository-key>` route.

## JFrog Package Feed Smoke

```bash
set -a
source /path/to/local/private/env
set +a

export ESHU_JFROG_PACKAGE_LIVE=1
export ESHU_JFROG_PACKAGE_METADATA_URL="${ESHU_JFROG_PACKAGE_METADATA_URL:?set an Artifactory package metadata wrapper URL}"
export ESHU_JFROG_PACKAGE_ECOSYSTEM="${ESHU_JFROG_PACKAGE_ECOSYSTEM:-npm}"
export ESHU_JFROG_PACKAGE_NAME="${ESHU_JFROG_PACKAGE_NAME:?set the package name in the metadata document}"
export ESHU_JFROG_PACKAGE_NAMESPACE="${ESHU_JFROG_PACKAGE_NAMESPACE:-}"
export ESHU_JFROG_PACKAGE_REGISTRY="${ESHU_JFROG_PACKAGE_REGISTRY:-${JFROG_PACKAGE_REGISTRY:-${JFROG_URL:-${JFROG_BASE_URL:-}}}}"
export ESHU_JFROG_PACKAGE_USERNAME="${ESHU_JFROG_PACKAGE_USERNAME:-${JFROG_USERNAME:-${JFROG_USER:-}}}"
export ESHU_JFROG_PACKAGE_PASSWORD="${ESHU_JFROG_PACKAGE_PASSWORD:-${JFROG_PASSWORD:-}}"
export ESHU_JFROG_PACKAGE_BEARER_TOKEN="${ESHU_JFROG_PACKAGE_BEARER_TOKEN:-${JFROG_ACCESS_TOKEN:-${JFROG_BEARER_TOKEN:-}}}"

cd go
go test ./internal/collector/packageregistry/packageruntime \
  -run TestLiveJFrogPackageFeed -count=1 -v
```

The package smoke is read-only and skips unless `ESHU_JFROG_PACKAGE_LIVE=1`.
It strips query strings and fragments from emitted source references and fails
if credential material appears in errors, source refs, or fact payloads. For
Maven, set `ESHU_JFROG_PACKAGE_NAMESPACE` to the package `groupId`.

## ECR Smoke

```bash
export ESHU_ECR_OCI_LIVE=1
export ESHU_ECR_OCI_REGION="us-east-1"
export ESHU_ECR_OCI_REGISTRY_ID="123456789012"
export ESHU_ECR_OCI_REPOSITORY="team/api"
export ESHU_ECR_OCI_REFERENCE="latest"

cd go
go test ./internal/collector/ociregistry/ecr -run TestLiveECR -count=1 -v
```

`ESHU_ECR_OCI_REFERENCE` is optional. Use
`ESHU_ECR_OCI_REGISTRY_HOST` when testing a nonstandard host shape.

## Docker Hub Smoke

```bash
export ESHU_DOCKERHUB_OCI_LIVE=1
export ESHU_DOCKERHUB_OCI_REPOSITORY="library/busybox"
export ESHU_DOCKERHUB_OCI_REFERENCE="latest"

cd go
go test ./internal/collector/ociregistry/dockerhub -run TestLiveDockerHub -count=1 -v
```

Set `ESHU_DOCKERHUB_OCI_USERNAME` and `ESHU_DOCKERHUB_OCI_PASSWORD` for private
repositories or anonymous rate-limit avoidance.

## GHCR Smoke

```bash
export ESHU_GHCR_OCI_LIVE=1
export ESHU_GHCR_OCI_REPOSITORY="stargz-containers/busybox"
export ESHU_GHCR_OCI_REFERENCE="1.32.0-org"

cd go
go test ./internal/collector/ociregistry/ghcr -run TestLiveGHCR -count=1 -v
```

Set `ESHU_GHCR_OCI_USERNAME` and `ESHU_GHCR_OCI_PASSWORD` for private GHCR
repositories or organization packages that deny anonymous pulls.

## Harbor Smoke

```bash
export ESHU_HARBOR_OCI_LIVE=1
export ESHU_HARBOR_OCI_URL="https://harbor.example.com"
export ESHU_HARBOR_OCI_REPOSITORY="project/image"
export ESHU_HARBOR_OCI_REFERENCE="latest"
export ESHU_HARBOR_OCI_USERNAME="robot$reader"
export ESHU_HARBOR_OCI_PASSWORD="local-secret"

cd go
go test ./internal/collector/ociregistry/harbor -run TestLiveHarbor -count=1 -v
```

## Google Artifact Registry Smoke

```bash
export ESHU_GAR_OCI_LIVE=1
export ESHU_GAR_OCI_REGISTRY_HOST="us-west1-docker.pkg.dev"
export ESHU_GAR_OCI_REPOSITORY="project-id/repository/image"
export ESHU_GAR_OCI_REFERENCE="latest"
export ESHU_GAR_OCI_USERNAME="oauth2accesstoken"
export ESHU_GAR_OCI_PASSWORD="local-access-token"

cd go
go test ./internal/collector/ociregistry/gar -run TestLiveGAR -count=1 -v
```

## Azure Container Registry Smoke

```bash
export ESHU_ACR_OCI_LIVE=1
export ESHU_ACR_OCI_REGISTRY_HOST="example.azurecr.io"
export ESHU_ACR_OCI_REPOSITORY="samples/artifact"
export ESHU_ACR_OCI_REFERENCE="latest"
export ESHU_ACR_OCI_USERNAME="00000000-0000-0000-0000-000000000000"
export ESHU_ACR_OCI_PASSWORD="local-access-token"

cd go
go test ./internal/collector/ociregistry/acr -run TestLiveACR -count=1 -v
```
