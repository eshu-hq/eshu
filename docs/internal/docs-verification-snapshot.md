# Docs Verification Snapshot

This file holds the detailed verification history for the documentation cleanup branch.


- Focused docs verification passed after the parallel remaining-docs compression
  for `docs/public/why-eshu.md`, `docs/internal/agent-guide.md`,
  `docs/public/reference/cli-reference.md`,
  `docs/public/reference/mcp-cookbook.md`, `go/internal/mcp`,
  `docs/public/reference/http-api/code.md`,
  `docs/public/reference/local-data-root-spec.md`,
  `docs/public/guides/collector-authoring.md`, and `go/cmd/reducer`, with 0
  contradicted and 0 missing evidence claims on each focused run.
- `go test ./cmd/eshu ./internal/mcp ./internal/query ./cmd/api ./cmd/reducer ./internal/reducer -count=1`
  passed after the parallel remaining-docs compression.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 173 documents, 1,228 claims, 0 contradicted, and 0 missing
  evidence claims after the parallel remaining-docs compression.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 561 documents, 1,626 claims, 0 contradicted, and 0 missing
  evidence claims after the parallel remaining-docs compression.
- `scripts/verify-package-docs.sh`, `git diff --check`, `cmp -s AGENTS.md CLAUDE.md`,
  and `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after the parallel remaining-docs compression.
- `go run ./cmd/eshu docs verify ../docs/public/run-locally/docker-compose.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 6 claims, 0 contradicted, and 0 missing evidence
  claims after the Docker Compose run-local compression. `git diff --check`
  and `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  also passed. Compose config rendering was not run because `docker` is not
  installed in this shell.
- Focused docs verification passed after the Terraform, Helm, backend, and
  package docs compression for `docs/public/guides/terraform-providers`,
  `docs/public/deploy/kubernetes`, `docs/public/reference/graph-backend-installation.md`,
  `docs/public/reference/graph-backend-operations.md`,
  `docs/public/reference/cypher-performance.md`, `go/cmd/bootstrap-index`,
  `go/internal/graph`, `go/internal/status`, `go/internal/relationships`,
  `go/internal/facts`, and `go/internal/telemetry`, with 0 contradicted and 0
  missing evidence claims on each focused run. The verifier reported only
  unsupported shell-command claim types for Terraform, Helm, and kubectl
  examples.
- `go test ./internal/terraformschema ./internal/relationships -count=1`,
  `go test ./internal/parser/hcl ./internal/parser -run 'HCL|Terraform|Classify|Provider' -count=1`,
  `go test ./cmd/eshu -run 'TestGraph|TestLocalGraph|TestNornicDB|TestInstall' -count=1`,
  `go test ./cmd/bootstrap-index -count=1`, and
  `go test ./internal/graph ./internal/status ./internal/facts ./internal/telemetry -count=1`
  passed after the Terraform, Helm, backend, and package docs compression.
- `helm lint ./deploy/helm/eshu`, default `helm template`, claim-driven
  package-registry collector render, GitHub webhook ingress render, and
  bundled-NornicDB reducer-lane render passed after the Helm values compression.
- `go run ./cmd/eshu docs verify ../go/internal/storage/postgres --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 3 documents, 4 claims, 0 contradicted, and 0 missing evidence
  claims after the Postgres scoped AGENTS compression.
- `go test ./internal/storage/postgres -count=1`, `scripts/verify-package-docs.sh`,
  and `git diff --check` passed after the Postgres scoped AGENTS compression.
- `go run ./cmd/eshu docs verify ../go/internal/storage/cypher --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 2 documents, 1 claim, 0 contradicted, and 0 missing evidence
  claims after the Cypher storage guide compression.
- `go test ./internal/storage/cypher -count=1`, `scripts/verify-package-docs.sh`,
  and `git diff --check` passed after the Cypher storage guide compression.
- `go run ./cmd/eshu docs verify ../docs/public/reference/fact-envelope-reference.md --limit 1200 --fail-on contradicted,missing_evidence`
  and `go run ./cmd/eshu docs verify ../docs/public/reference/fact-schema-versioning.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 0 contradicted and 0 missing evidence claims after the fact
  contract rewrite.
- `go run ./cmd/eshu docs verify ../docs/public/reference/collector-reducer-readiness.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 1 claim, 0 contradicted, and 0 missing evidence
  claims after the readiness rewrite.
- Focused docs verification passed for `go/internal/projector`,
  `go/internal/runtime`, `go/cmd/ingester`, `go/internal/workflow`,
  `go/internal/query`, and `go/internal/coordinator` after the package README
  compression.
- Focused docs verification passed for `docs/public/reference/telemetry/traces.md`
  and `docs/public/reference/telemetry/cross-service-correlation.md`; the public
  reference polish subagent also verified the truth-label, MCP cookbook,
  documentation updater, local performance, and NornicDB pitfalls pages with 0
  contradicted and 0 missing evidence claims.
- `go test ./internal/projector ./internal/runtime ./cmd/ingester ./internal/workflow ./internal/query ./internal/coordinator ./internal/facts ./internal/component -count=1`
  passed after integrating the subagent docs batch.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 173 documents, 1,217 claims, 0 contradicted, and 0 missing
  evidence claims after the subagent docs batch.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 561 documents, 1,619 claims, 0 contradicted, and 0 missing
  evidence claims after the subagent docs batch.
- `scripts/verify-package-docs.sh`, `git diff --check`, `cmp -s AGENTS.md CLAUDE.md`,
  and `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after the subagent docs batch. The package-doc verifier reported no
  changed Go package source files.
- `go run ./cmd/eshu docs verify ../docs/public/run-locally/docker-compose.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 6 claims, 0 contradicted, and 0 missing evidence
  claims after the Compose docs rewrite.
- `go run ./cmd/eshu docs verify ../docs/public/run-locally/local-binaries.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 17 claims, 0 contradicted, and 0 missing evidence
  claims after correcting local service and MCP binary ownership.
- `go run ./cmd/eshu docs verify ../docs/public/deploy/kubernetes --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 12 documents, 28 claims, 0 contradicted, and 0 missing evidence
  claims after the Helm/Kubernetes docs rewrite. The verifier reported only
  unsupported shell-command claim types for `helm` and `kubectl`.
- `helm lint ./deploy/helm/eshu`, `helm template eshu ./deploy/helm/eshu`,
  Prometheus ServiceMonitor render, bundled-NornicDB render with Helm hooks
  disabled, AWS freshness webhook render, and active Terraform-state collector
  render passed after the Helm docs rewrite. Helm lint reported only the
  chart-icon recommendation.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 181 documents, 1,230 claims, 0 contradicted, and 0 missing
  evidence claims after the Compose/Helm/local-binary docs rewrite.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 569 documents, 1,689 claims, 0 contradicted, and 0 missing
  evidence claims after the Compose/Helm/local-binary docs rewrite.
- `scripts/verify-package-docs.sh`, `git diff --check`, `cmp -s AGENTS.md CLAUDE.md`,
  and `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after the Compose/Helm/local-binary docs rewrite. The package-doc
  verifier reported no changed Go package source files. Docker Compose config
  expansion was not run because `docker` is not installed in this shell.
- `go run ./cmd/eshu docs verify ../docs/public/reference/service-workflows.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 0 claims, 0 contradicted, and 0 missing evidence
  claims after the service runtime workflow rewrite.
- `go run ./cmd/eshu docs verify ../docs/public/services --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 9 documents, 62 claims, 0 contradicted, and 0 missing evidence
  claims after the service runtime workflow rewrite.
- `go run ./cmd/eshu docs verify ../go/cmd/api --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 2 documents, 33 claims, 0 contradicted, and 0 missing evidence
  claims after the service runtime workflow rewrite.
- `go run ./cmd/eshu docs verify ../go/cmd/reducer --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 2 documents, 52 claims, 0 contradicted, and 0 missing evidence
  claims after the service runtime workflow rewrite.
- `go test ./cmd/api ./cmd/ingester ./cmd/reducer ./cmd/bootstrap-index ./cmd/workflow-coordinator ./internal/runtime ./internal/workflow ./internal/coordinator -count=1`
  passed after the service runtime workflow rewrite.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 181 documents, 1,227 claims, 0 contradicted, and 0 missing
  evidence claims after the service runtime workflow rewrite.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 569 documents, 1,685 claims, 0 contradicted, and 0 missing
  evidence claims after the service runtime workflow rewrite.
- `scripts/verify-package-docs.sh`, `git diff --check`, and
  `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after the service runtime workflow rewrite. The package-doc verifier
  reported no changed Go package source files.
- `go run ./cmd/eshu docs verify ../docs/public/reference/cli-reference.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 108 claims, 0 contradicted, and 0 missing evidence
  claims after the CLI reference rewrite.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 181 documents, 1,227 claims, 0 contradicted, and 0 missing
  evidence claims after the CLI reference rewrite.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 569 documents, 1,681 claims, 0 contradicted, and 0 missing
  evidence claims after the CLI reference rewrite.
- `go test ./cmd/eshu -count=1`, `scripts/verify-package-docs.sh`, and
  `git diff --check` passed after the CLI reference rewrite. The package-doc
  verifier reported no changed Go package source files.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after the CLI reference rewrite.
- `go run ./cmd/eshu docs verify ../docs/public/reference/configuration.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 14 claims, 0 contradicted, and 0 missing evidence
  claims after the configuration reference rewrite.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 181 documents, 1,250 claims, 0 contradicted, and 0 missing
  evidence claims after the configuration reference rewrite.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 569 documents, 1,704 claims, 0 contradicted, and 0 missing
  evidence claims after the configuration reference rewrite.
- `go test ./cmd/eshu -run 'TestConfig|TestRootVersion|TestDocsVerifyCommandIsRegistered' -count=1`,
  `scripts/verify-package-docs.sh`, `git diff --check`, and
  `cmp -s AGENTS.md CLAUDE.md` passed after the configuration reference
  rewrite. The package-doc verifier reported no changed Go package source
  files.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after the configuration reference rewrite.
- `go run ./cmd/eshu docs verify ../docs/public/deploy/kubernetes/helm-collector-and-webhook-values.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 4 claims, 0 contradicted, and 0 missing evidence
  claims after the Helm collector/webhook values rewrite.
- `helm template eshu ./deploy/helm/eshu` and
  `helm lint ./deploy/helm/eshu` passed after the Helm collector/webhook values
  rewrite. Helm lint reported only the chart-icon recommendation.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 569 documents, 1,751 claims, 0 contradicted, and 0 missing
  evidence claims after the bootstrap-index docs and copied-image cleanup.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 181 documents, 1,300 claims, 0 contradicted, and 0 missing
  evidence claims after the bootstrap-index docs and copied-image cleanup.
- `go run ./cmd/eshu docs verify ../go/cmd/bootstrap-index --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 2 documents, 24 claims, 0 contradicted, and 0 missing evidence
  claims after the bootstrap-index docs cleanup.
- `go run ./cmd/eshu docs verify ../docs/public/services/bootstrap-index.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 5 claims, 0 contradicted, and 0 missing evidence
  claims after the bootstrap-index public service cleanup.
- `go test ./cmd/bootstrap-index -count=1` passed after comparing the
  bootstrap-index docs against current command code and tests.
- `go test ./cmd/eshu -count=1`, `scripts/verify-package-docs.sh`,
  `git diff --cached --check`, and `cmp -s AGENTS.md CLAUDE.md` passed after
  the bootstrap-index docs cleanup.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after deleting the copied image assets.
- `rg -n "docs/public/images|public/images|\\.\\./images|\\./images|images/[A-Za-z0-9_.-]+\\.(png|jpg|jpeg|gif|webp|svg)|!\\[[^]]*\\]\\([^)]*\\.(png|jpg|jpeg|gif|webp|svg)[^)]*\\)" docs/public docs/mkdocs.yml README.md`
  returned no source-doc references after deleting copied image assets.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 569 documents, 1,756 claims, 0 contradicted, and 0 missing
  evidence claims after the documentation updater actuator contract rewrite.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 181 documents, 1,307 claims, 0 contradicted, and 0 missing
  evidence claims after the documentation updater actuator contract rewrite.
- `go run ./cmd/eshu docs verify ../docs/public/reference/documentation-updater-actuator-contract.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 8 claims, 0 contradicted, and 0 missing evidence
  claims after the documentation updater actuator contract rewrite.
- `go test ./internal/query -run 'TestDocumentation|TestContentReaderDocumentation|TestBuildDocumentation' -count=1`,
  `go test ./internal/mcp -run 'TestDocumentation|TestReadOnlyTools' -count=1`,
  `go test ./cmd/eshu -count=1`, `scripts/verify-package-docs.sh`,
  `git diff --check`, and `cmp -s AGENTS.md CLAUDE.md` passed after the
  documentation updater actuator contract rewrite.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after the documentation updater actuator contract rewrite.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 569 documents, 1,751 claims, 0 contradicted, and 0 missing
  evidence claims after the MCP cookbook rewrite.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 181 documents, 1,302 claims, 0 contradicted, and 0 missing
  evidence claims after the MCP cookbook rewrite.
- `go run ./cmd/eshu docs verify ../docs/public/reference/mcp-cookbook.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 0 claims, 0 contradicted, and 0 missing evidence
  claims after the MCP cookbook rewrite.
- `go run ./cmd/eshu docs verify ../go/internal/mcp --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 2 documents, 4 claims, 0 contradicted, and 0 missing evidence
  claims after the MCP cookbook rewrite.
- `go test ./internal/mcp -run 'TestMCPCookbook|TestReadOnlyTools' -count=1`,
  `go test ./internal/mcp -count=1`, `go test ./cmd/eshu -count=1`,
  `scripts/verify-package-docs.sh`, `git diff --check`, and
  `cmp -s AGENTS.md CLAUDE.md` passed after the MCP cookbook rewrite.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after the MCP cookbook rewrite.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 569 documents, 1,751 claims, 0 contradicted, and 0 missing
  evidence claims after the telemetry logs and correlation rewrite.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 181 documents, 1,302 claims, 0 contradicted, and 0 missing
  evidence claims after the telemetry logs and correlation rewrite.
- `go run ./cmd/eshu docs verify ../docs/public/reference/telemetry/logs.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 0 claims, 0 contradicted, and 0 missing evidence
  claims after the telemetry logs and correlation rewrite.
- `go run ./cmd/eshu docs verify ../docs/public/reference/telemetry/cross-service-correlation.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 0 claims, 0 contradicted, and 0 missing evidence
  claims after the telemetry logs and correlation rewrite.
- `go test ./internal/telemetry -count=1`, `go test ./cmd/eshu -count=1`,
  `scripts/verify-package-docs.sh`, `git diff --check`, and
  `cmp -s AGENTS.md CLAUDE.md` passed after the telemetry logs and
  correlation rewrite.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after the telemetry logs and correlation rewrite.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 569 documents, 1,753 claims, 0 contradicted, and 0 missing
  evidence claims after the capability conformance spec rewrite.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 181 documents, 1,304 claims, 0 contradicted, and 0 missing
  evidence claims after the capability conformance spec rewrite.
- `go run ./cmd/eshu docs verify ../docs/public/reference/capability-conformance-spec.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 2 claims, 0 contradicted, and 0 missing evidence
  claims after the capability conformance spec rewrite.
- `go run ./cmd/eshu docs verify ../docs/public/reference/backend-conformance.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 8 claims, 0 contradicted, and 0 missing evidence
  claims after the capability conformance spec rewrite.
- `go run ./cmd/eshu docs verify ../docs/public/reference/truth-label-protocol.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 0 claims, 0 contradicted, and 0 missing evidence
  claims after the capability conformance spec rewrite.
- `go test ./internal/query -run TestCapabilityMatrixMatchesYAMLContract -count=1`,
  `go test ./internal/backendconformance -count=1`,
  `go test ./cmd/eshu -count=1`, `scripts/verify-package-docs.sh`,
  `git diff --check`, and `cmp -s AGENTS.md CLAUDE.md` passed after the
  capability conformance spec rewrite.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after the capability conformance spec rewrite.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 569 documents, 1,751 claims, 0 contradicted, and 0 missing
  evidence claims after the telemetry package README rewrite.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 181 documents, 1,302 claims, 0 contradicted, and 0 missing
  evidence claims after the telemetry package README rewrite.
- `go run ./cmd/eshu docs verify ../go/internal/telemetry --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 2 documents, 0 claims, 0 contradicted, and 0 missing evidence
  claims after the telemetry package README rewrite.
- `go test ./internal/telemetry -count=1`, `go test ./cmd/eshu -count=1`,
  `git diff --check`, and `cmp -s AGENTS.md CLAUDE.md` passed after the
  telemetry package README rewrite.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after the telemetry package README rewrite.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 569 documents, 1,750 claims, 0 contradicted, and 0 missing
  evidence claims after the Terraform-state collector service split.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 181 documents, 1,302 claims, 0 contradicted, and 0 missing
  evidence claims after the Terraform-state collector service split.
- `go run ./cmd/eshu docs verify ../docs/public/services --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 9 documents, 69 claims, 0 contradicted, and 0 missing evidence
  claims after the Terraform-state collector service split.
- `go run ./cmd/eshu docs verify ../docs/public/reference/environment-collectors.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 69 claims, 0 contradicted, and 0 missing evidence
  claims after adding `ESHU_TERRAFORM_SCHEMA_DIR`.
- `go test ./cmd/collector-terraform-state ./internal/collector/terraformstate ./internal/collector/tfstateruntime -count=1`,
  `go test ./cmd/eshu -count=1`, `git diff --check`, and
  `cmp -s AGENTS.md CLAUDE.md` passed after the Terraform-state collector
  service split. `scripts/verify-package-docs.sh` reported no changed Go
  package source files.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after adding the Terraform-state collector split pages to navigation.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 567 documents, 1,746 claims, 0 contradicted, and 0 missing
  evidence claims after the AWS cloud collector service split.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 179 documents, 1,298 claims, 0 contradicted, and 0 missing
  evidence claims after the AWS cloud collector service split.
- `go run ./cmd/eshu docs verify ../docs/public/services --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 7 documents, 66 claims, 0 contradicted, and 0 missing evidence
  claims after the AWS cloud collector service split.
- `go test ./cmd/collector-aws-cloud ./internal/collector/awscloud/... -count=1`,
  `go test ./cmd/eshu -count=1`, `scripts/verify-package-docs.sh`,
  `git diff --check`, and `cmp -s AGENTS.md CLAUDE.md` passed after the AWS
  cloud collector service split.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after adding the AWS cloud collector split pages to navigation.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 565 documents, 1,751 claims, 0 contradicted, and 0 missing
  evidence claims after the Cypher package README rewrite.
- `go run ./cmd/eshu docs verify ../go/internal/storage/cypher --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 2 documents, 1 claim, 0 contradicted, and 0 missing evidence
  claims after the Cypher package README rewrite.
- `go test ./internal/storage/cypher -count=1`, `go test ./cmd/eshu -count=1`,
  `git diff --check`, and `cmp -s AGENTS.md CLAUDE.md` passed after the Cypher
  package README rewrite.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 565 documents, 1,748 claims, 0 contradicted, and 0 missing
  evidence claims after the Helm values split.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 177 documents, 1,303 claims, 0 contradicted, and 0 missing
  evidence claims after the Helm values split.
- `go run ./cmd/eshu docs verify ../docs/public/deploy/kubernetes --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 12 documents, 27 claims, 0 contradicted, and 0 missing evidence
  claims after the Helm values split.
- `helm template eshu ./deploy/helm/eshu` and
  `helm lint ./deploy/helm/eshu` passed after the Helm values split. Helm lint
  reported only the chart-icon recommendation.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after adding the split Helm values pages to navigation.
- `go test ./cmd/eshu -count=1`, `git diff --check`, and
  `cmp -s AGENTS.md CLAUDE.md` passed after the Helm values split.
- `go run ./cmd/eshu docs verify .. --limit 2000 --fail-on contradicted,missing_evidence`
  passed with 562 documents, 1,748 claims, 0 contradicted, and 0 missing
  evidence claims after rebasing onto `a0d676f`.
- `go run ./cmd/eshu docs verify ../docs/public --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 174 documents, 1,299 claims, 0 contradicted, and 0 missing
  evidence claims after rebasing onto `a0d676f`.
- `go run ./cmd/eshu docs verify ../docs/public/reference --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 74 documents, 1,049 claims, 0 contradicted, and 0 missing
  evidence claims after rebasing onto `a0d676f`.
- `go test ./cmd/eshu -count=1`
  passed after teaching the docs verifier to read split `environment-*.md`
  reference pages and after rebasing onto `a0d676f`.
- `go run ./cmd/eshu docs verify ../docs/public/reference/telemetry --limit 1000 --fail-on contradicted,missing_evidence`
  passed with 10 documents, 2 claims, 0 contradicted, and 0 missing evidence
  claims after correcting the status CLI reference to `eshu-admin-status`.
- `go run ./cmd/eshu docs verify ../docs/public/deployment --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 8 documents, 33 claims, 0 contradicted, and 0 missing evidence
  claims after the service runtime split.
- `go run ./cmd/eshu docs verify ../docs/public/architecture.md --limit 1200 --fail-on contradicted,missing_evidence`
  passed with 1 document, 1 claim, 0 contradicted, and 0 missing evidence
  claims after the architecture rewrite.
- Focused verifier tests passed for package docs, collector authoring, and
  repository documentation ownership after the scoped `AGENTS.md` restore.
- `uv run --with mkdocs --with mkdocs-material --with pymdown-extensions mkdocs build --strict --clean --config-file docs/mkdocs.yml`
  passed after adding the environment-variable split pages to navigation and
  repairing the Docker Compose route-map link, then passed again after
  rebasing onto `a0d676f`.
- `git diff --check` passed after rebasing onto `a0d676f`.
- `cmp -s AGENTS.md CLAUDE.md` passed after the environment-variable split.
  It passed again after rebasing onto `a0d676f`.
