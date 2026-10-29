# eshu-deploy change proposal: adopt the extracted contract tests and proof scripts

Source: eshu @ `2e3ba9fc40` (paths as deleted by the #8871 in-tree removal).
Destination base: eshu-deploy @ `bc6b17897056` (main after PR #11 merge; includes PR #5 plus the review-thread follow-up: missing files, translated paths, Go lane, exec bits).
Full adapted tree, verified as below: `/tmp/eshu-deploy-proposal`
(70 files). Nothing here was pushed to eshu-deploy.

## What moves and where

New paths mirror the dest layout (`helm/`, `compose/`, `docs/`,
`gitops/`, `scripts/`). Go tests land in `go/internal/runtime/` (new Go
package; dest adopts the module wiring).

### Go contract tests (33 files, dest `go/internal/runtime/`)

Verbatim from the eshu source commit, except the noted line translations.
Verified: `go vet` clean against the real eshu module.

- compose_component_test.go, compose_graph_write_env_test.go,
  compose_large_repo_defaults_test.go, compose_neo4j_image_test.go,
  compose_remote_e2e_observability_preflight_test.go,
  compose_shared_projection_defaults_test.go: verbatim.
- compose_remote_e2e_code_call_test.go: verbatim (`.env.remote-e2e.example`
  sits at the dest root, same as eshu).
- compose_remote_e2e_split_test.go: `readRepositoryFile(t, "../../..",
  "docker-compose.remote-e2e.yaml")` reads
  `deployRepositoryRoot(t), "compose/docker-compose.remote-e2e.yaml"`.
- compose_semantic_test.go: `"docs/public/run-locally/docker-compose.md"`
  becomes `"docs/run-locally/docker-compose.md"` (same helper root).
- helm_chart_core/ingester/scip_workers/vulnerability/cicd_run/claim/
  component_activations(_contract,_core_version,_csv_lists,_validation,
  _script)/component_extension/gcp_freshness_webhook/governance_auth/
  jira/live/nornicdb/observability/pagerduty/reducer_lanes/
  sbom_attestation/security_alert/webhook_listener/writer_readyz_probe:
  verbatim, except:
  - ingester: chart path `deploy/helm/eshu/values.yaml` becomes
    `helm/eshu/values.yaml` (dest root).
  - scip_workers: schema path likewise `helm/eshu/values.schema.json`.
  - governance_auth: ci values path becomes
    `helm/eshu/ci/governance-two-team-k8s.values.yaml`.
  - gcp_freshness_webhook: the two map keys become
    `helm/eshu/values.yaml` / `helm/eshu/values.schema.json` (dest root).

### Go helpers (4 files, dest `go/internal/runtime/`)

- deploy_checkout_test.go: NEW, full content below. No pin: dest tests
  assert the dest tree itself.
- helm_chart_contract_test.go: verbatim eshu copy minus `repositoryRoot`;
  both `chartPath` lines become
  `filepath.Join(deployRepositoryRoot(t), "helm", "eshu")`.
- compose_document_test.go: verbatim eshu copy; reads resolve via
  `deployComposePath(t, ...)` and
  `readRepositoryFile(t, deployRepositoryRoot(t), filepath.Join("compose", ...))`.
- deploy_assert_test.go: verbatim copies of `requireComposeService`,
  `assertComposeEnv`, `graphRuntimeServices`, `readRepositoryFile`,
  `assertComposeVolumeContains`, `assertComposeScriptContains`,
  `assertComposeDependency`, `composeDependencyExists`,
  `assertHelmReadOnlyVolumeMount`, `digestedImageRef` (with comment) from
  the eshu sources, plus the `composeDocument`/`composeService` types.

```go
// deploy_checkout_test.go (dest copy, full content)
package runtime

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func deployRepositoryRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test filename")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "helm", "eshu", "Chart.yaml")); err != nil {
		t.Fatalf("deploy repository root %s has no helm/eshu/Chart.yaml: %v", root, err)
	}
	return root
}

func deployChartPath(t *testing.T, name string) string {
	t.Helper()

	return filepath.Join(deployRepositoryRoot(t), "helm", "eshu", name)
}

func deployComposePath(t *testing.T, name string) string {
	t.Helper()

	return filepath.Join(deployRepositoryRoot(t), "compose", name)
}

func deployDocPath(t *testing.T, name string) string {
	t.Helper()

	return filepath.Join(deployRepositoryRoot(t), "docs", name)
}
```

### Shell proof scripts (33 files, dest `scripts/` + `scripts/lib/`)

Translated from the eshu source by this exact map (verified: no
remaining in-tree `deploy/`, `docs/public/`, or root-compose reference
outside `eshu-deploy` mentions; `bash -n` clean on every file):

- `${repo_root}/deploy/helm/eshu` to `${repo_root}/helm/eshu`
- `${repo_root}/deploy/argocd|manifests|observability|grafana` to
  `${repo_root}/gitops/argocd|manifests`, `${repo_root}/observability`,
  `${repo_root}/grafana`
- bare `deploy/helm/eshu|argocd|manifests|observability|grafana` to the
  same dest-relative trees; `docs/public/deploy/kubernetes` to
  `docs/kubernetes`
- `${repo_root}/docker-compose.<name>.y(a)ml` to
  `${repo_root}/compose/docker-compose.<name>.y(a)ml`
- `docs/public/run-locally/docker-compose.component-extension*.yaml` and
  `docker-compose.governance-two-team.yaml` to
  `compose/fragments/`; `docs/public/run-locally/docker-compose.md` to
  `docs/run-locally/docker-compose.md`

Files: parity driver + 2 fakes, 13 deploy-proof verifiers + 7 drivers +
5 driver fixtures, k8s proof + manifests lib + provenance lib +
provenance driver, storage verifier + driver + bad-md fixture,
`.env.example` (dest root, verbatim).
Exceptions, all flagged for dest-owner review:

- Parity driver fixture tree is rebuilt in the dest layout (helm/,
  compose/, root env file); the gate already accepts `--repo-root`.
- run-k8s proof builds the image from eshu's Dockerfile; dest has none.
  Replace that step with the published image or a second checkout.
- `.env.example` still names the retired template compose file removed
  from core by this change; dest owner must repoint that reference at the
  `compose/` layout on import.
- The parity script itself is already seeded in dest; not repeated here.

### Governance lockstep assertion (new dest snippet)

The k8s proof lockstep-image check removed from eshu belongs with the
proof in dest:

```bash
chartRef="$(yq -r '.nornicdb.image.repository + ":" + .nornicdb.image.tag' helm/eshu/values.yaml)"
proof="$(cat scripts/run-k8s-two-team-governance-proof.sh)"
[[ "$proof" == *"nornicdb_image=\"$chartRef\""* ]] ||
  { echo "proof script does not deploy the lockstep NornicDB image $chartRef" >&2; exit 1; }
```

## Verification (this machine)

- Proposal Go tree: `go vet ./internal/runtime/` clean (checked against
  the real eshu module via replace directive); `gofmt -l` empty.
- Proposal scripts: `bash -n` clean on all 33; stray in-tree path scan
  clean; fakes byte-identical to eshu.
- eshu side: moved Go files deleted only after proving no staying file
  uses their helpers (two restores: sbom preflight, postgres budget).
- Chart lane proof: `0.0.3-pre-release-26` re-pulled from
  `oci://ghcr.io/eshu-hq/charts/eshu` at execution (digest in handoff).

## No-Regression Evidence: in-tree removal

- Baseline: `origin/main` (`4750c37f8b` at rebase); dest checkout pin
  `bc6b17897056e3c5dab27dd2357a013869b2bd9a` (`go/internal/deploy/checkout_test.go:18`).
- Change shape: deploy assets, root Compose files, env examples, deploy
  docs, and proof scripts deleted (~37k lines across ~380 files); staying
  Go changes are additive only (fail-closed `ESHU_DEPLOY_CHECKOUT`
  helpers, two `FailsClosed` probe tests, one `envregistry` entry).
- After measurement: `go test ./internal/deploy/ ./internal/cli/demo/
  ./internal/runtime/` exit 0 with `ESHU_DEPLOY_CHECKOUT` at the pin;
  `go test ./internal/envregistry/` exit 0;
  `bash scripts/verify-docs-cli-env-refs.sh` OK (1868 references
  checked); `mkdocs build --strict --clean` exit 0; `git diff --check`
  clean.
- Backend/version: no backend, schema, queue, or hot-path change; no
  NornicDB/Neo4j knob touched. Census counts stay frozen extraction
  evidence (102 templates, 11 argocd, 6 manifests, 8 observability,
  3 dashboards, 18 deploy docs, 9 run-locally).
- No-Observability-Change: census tests emit no metrics, spans, or logs
  beyond `go test` output; no new instrumentation added, none removed
  from a live path.
- Why safe: no runtime code path is modified, so there is no hot path to
  regress; every staying reader of a deleted asset addresses the pinned
  dest checkout or fails closed naming `ESHU_DEPLOY_CHECKOUT`.
