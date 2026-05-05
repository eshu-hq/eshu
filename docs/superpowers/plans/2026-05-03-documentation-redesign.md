# Documentation Redesign Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reorganize Eshu documentation around human task paths: local use, MCP, Kubernetes deployment, operations, concepts, extension, and reference.

**Architecture:** Keep existing accurate content, but change the entry points and navigation. Move and split pages in small chunks so every chunk can be verified with MkDocs and stale-term scans.

**Tech Stack:** MkDocs Material, Markdown, existing Eshu docs under `docs/docs`, repository root `README.md`, and `docs/mkdocs.yml`.

---

## File map

Primary files to modify:

- `docs/mkdocs.yml` - top-level navigation and page grouping.
- `docs/docs/index.md` - new human entry page.
- `README.md` - compact repository entry with links into the docs.

New or renamed docs to create:

- `docs/docs/start-here.md`
- `docs/docs/run-locally/index.md`
- `docs/docs/run-locally/local-binaries.md`
- `docs/docs/run-locally/docker-compose.md`
- `docs/docs/run-locally/mcp-local.md`
- `docs/docs/use/index.md`
- `docs/docs/use/index-repositories.md`
- `docs/docs/use/code-questions.md`
- `docs/docs/use/trace-infrastructure.md`
- `docs/docs/mcp/index.md`
- `docs/docs/deploy/kubernetes/index.md`
- `docs/docs/deploy/kubernetes/prerequisites.md`
- `docs/docs/deploy/kubernetes/storage.md`
- `docs/docs/deploy/kubernetes/helm-quickstart.md`
- `docs/docs/deploy/kubernetes/helm-values.md`
- `docs/docs/deploy/kubernetes/manifests.md`
- `docs/docs/deploy/kubernetes/argocd.md`
- `docs/docs/deploy/kubernetes/production-checklist.md`
- `docs/docs/deploy/kubernetes/upgrades-rollbacks.md`
- `docs/docs/operate/index.md`
- `docs/docs/operate/health-checks.md`
- `docs/docs/operate/telemetry.md`
- `docs/docs/operate/troubleshooting.md`
- `docs/docs/understand/index.md`
- `docs/docs/extend/index.md`

Existing pages to preserve but potentially move or split:

- `docs/docs/getting-started/quickstart.md`
- `docs/docs/getting-started/installation.md`
- `docs/docs/deployment/docker-compose.md`
- `docs/docs/deployment/overview.md`
- `docs/docs/deployment/helm.md`
- `docs/docs/deployment/manifests.md`
- `docs/docs/deployment/argocd.md`
- `docs/docs/guides/mcp-guide.md`
- `docs/docs/guides/starter-prompts.md`
- `docs/docs/reference/cli-reference.md`
- `docs/docs/reference/local-testing.md`
- `docs/docs/reference/environment-variables.md`
- `docs/docs/architecture.md`
- `docs/docs/concepts/how-it-works.md`
- `docs/docs/concepts/graph-model.md`

Do not edit generated `docs/site` files by hand.

---

## Chunk 1: Navigation skeleton and start page

**Files:**

- Modify: `docs/mkdocs.yml`
- Modify: `docs/docs/index.md`
- Create: `docs/docs/start-here.md`

- [ ] **Step 1: Add the new nav skeleton**

Update `docs/mkdocs.yml` so the top-level sections are:

```yaml
nav:
  - Home:
    - Overview: index.md
    - Start Here: start-here.md
  - Run Locally:
    - Choose Your Local Path: run-locally/index.md
    - Local Binaries: run-locally/local-binaries.md
    - Docker Compose: run-locally/docker-compose.md
    - Local MCP: run-locally/mcp-local.md
  - Use Eshu:
    - Overview: use/index.md
    - Index Repositories: use/index-repositories.md
    - Ask Code Questions: use/code-questions.md
    - Trace Infrastructure: use/trace-infrastructure.md
    - Starter Prompts: guides/starter-prompts.md
  - Connect MCP:
    - Overview: mcp/index.md
    - MCP Guide: guides/mcp-guide.md
    - MCP Reference: reference/mcp-reference.md
    - MCP Cookbook: reference/mcp-cookbook.md
  - Deploy to Kubernetes:
    - Overview: deploy/kubernetes/index.md
    - Prerequisites: deploy/kubernetes/prerequisites.md
    - Storage: deploy/kubernetes/storage.md
    - Helm Quickstart: deploy/kubernetes/helm-quickstart.md
    - Helm Values: deploy/kubernetes/helm-values.md
    - Minimal Manifests: deploy/kubernetes/manifests.md
    - Argo CD / GitOps: deploy/kubernetes/argocd.md
    - Production Checklist: deploy/kubernetes/production-checklist.md
    - Upgrade and Rollback: deploy/kubernetes/upgrades-rollbacks.md
```

Keep `Operate Eshu`, `Understand Eshu`, `Extend Eshu`, `Reference`, and `Project`
in the same file after these sections.

- [ ] **Step 2: Write `start-here.md`**

Use direct, human prose:

```markdown
# Start here

Eshu has three common starting points.

If you want Eshu on your laptop, start with [Run Locally](run-locally/index.md).
That page helps you choose between local binaries and Docker Compose.

If you want an assistant to query Eshu, start with [Connect MCP](mcp/index.md).
You can use MCP against the local owner, the Compose service, or a deployed API.

If you are deploying Eshu for a team, start with
[Deploy to Kubernetes](deploy/kubernetes/index.md). That path covers storage,
Helm, manifests, health checks, and production readiness.
```

- [ ] **Step 3: Humanize `index.md`**

Rewrite `docs/docs/index.md` so it is short and sends readers to the three
paths. Avoid long architecture explanations on the home page. Keep the value
statement, but remove inflated phrasing and generic lists.

- [ ] **Step 4: Verify docs build**

Run:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

Expected: exit 0.

- [ ] **Step 5: Run whitespace check**

Run:

```bash
git diff --check
```

Expected: no output, exit 0.

---

## Chunk 2: Split local setup into binaries and Compose

**Files:**

- Create: `docs/docs/run-locally/index.md`
- Create: `docs/docs/run-locally/local-binaries.md`
- Create: `docs/docs/run-locally/docker-compose.md`
- Create: `docs/docs/run-locally/mcp-local.md`
- Modify or retire: `docs/docs/getting-started/quickstart.md`
- Modify or retire: `docs/docs/getting-started/installation.md`
- Modify or redirect from: `docs/docs/deployment/docker-compose.md`

- [ ] **Step 1: Write local path chooser**

Create `run-locally/index.md` with a simple decision table:

```markdown
# Run locally

Choose the local path by what you are trying to prove.

| Path | Use it when | Starts |
| --- | --- | --- |
| Local binaries | You are developing Eshu or want one workspace owner | embedded Postgres, NornicDB, ingester, reducer |
| Docker Compose | You want the full laptop service stack | Postgres, graph backend, API, MCP, ingester, reducer |
```

- [ ] **Step 2: Write local binaries page**

Create `run-locally/local-binaries.md`. Include the validated build commands:

```bash
cd go
go build -o ./bin/eshu ./cmd/eshu
go build -o ./bin/eshu-api ./cmd/api
go build -o ./bin/eshu-mcp-server ./cmd/mcp-server
go build -o ./bin/eshu-bootstrap-index ./cmd/bootstrap-index
go build -o ./bin/eshu-ingester ./cmd/ingester
go build -o ./bin/eshu-reducer ./cmd/reducer
export PATH="$PWD/bin:$PATH"
```

Then include:

```bash
eshu install nornicdb
eshu graph start --workspace-root /path/to/repo
```

State that `eshu graph start` is foreground and owns embedded Postgres,
NornicDB, ingester, and reducer. State that HTTP read commands need an API
process unless they are attached through a supported local workflow.

- [ ] **Step 3: Write Docker Compose page**

Create `run-locally/docker-compose.md`. Put both official commands near the
top:

```bash
docker compose up --build
docker compose -f docker-compose.neo4j.yml up --build
```

Document that default Compose uses NornicDB and Postgres, while the Neo4j file
is the explicit Neo4j path.

- [ ] **Step 4: Write local MCP page**

Create `run-locally/mcp-local.md`. Explain the three local MCP shapes:

- local owner via `eshu mcp start --workspace-root <repo>`
- Compose MCP service on port `8081`
- MCP client setup via `eshu mcp setup`

- [ ] **Step 5: Convert old getting-started pages into redirects or short bridges**

Keep the old pages only if needed for existing links. They should point to the
new task pages and avoid duplicating command blocks.

- [ ] **Step 6: Verify**

Run MkDocs strict build and `git diff --check`.

---

## Chunk 3: Build the Kubernetes deployment lane

**Files:**

- Create: `docs/docs/deploy/kubernetes/index.md`
- Create: `docs/docs/deploy/kubernetes/prerequisites.md`
- Create: `docs/docs/deploy/kubernetes/storage.md`
- Create: `docs/docs/deploy/kubernetes/helm-quickstart.md`
- Create: `docs/docs/deploy/kubernetes/helm-values.md`
- Create: `docs/docs/deploy/kubernetes/manifests.md`
- Create: `docs/docs/deploy/kubernetes/argocd.md`
- Create: `docs/docs/deploy/kubernetes/production-checklist.md`
- Create: `docs/docs/deploy/kubernetes/upgrades-rollbacks.md`
- Modify or retire: existing `docs/docs/deployment/*.md`
- Modify: `deploy/helm/eshu/README.md`

- [ ] **Step 1: Write deployment overview**

`deploy/kubernetes/index.md` should answer:

- what gets deployed
- what runs as Deployment, StatefulSet, Job, or init step
- what external storage is required
- what page to read next

- [ ] **Step 2: Write storage page**

`deploy/kubernetes/storage.md` must state:

- NornicDB is the default graph backend
- Neo4j is the explicit supported graph backend
- Postgres is the relational database for facts, queues, status, content, and
  recovery data
- unsupported graph backends are not official

- [ ] **Step 3: Write Helm quickstart**

Move the practical Helm install flow out of generic deployment pages and into
`helm-quickstart.md`.

- [ ] **Step 4: Write production checklist**

Include storage readiness, secrets, resource requests, probes, telemetry,
backup/restore posture, and upgrade/rollback notes.

- [ ] **Step 5: Verify**

Run:

```bash
helm lint deploy/helm/eshu
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

---

## Chunk 4: Separate usage from reference

**Files:**

- Create: `docs/docs/use/index.md`
- Create: `docs/docs/use/index-repositories.md`
- Create: `docs/docs/use/code-questions.md`
- Create: `docs/docs/use/trace-infrastructure.md`
- Create: `docs/docs/mcp/index.md`
- Modify: `docs/docs/guides/starter-prompts.md`
- Modify: `docs/docs/guides/mcp-guide.md`
- Modify or split: `docs/docs/reference/cli-reference.md`

- [ ] **Step 1: Write `Use Eshu` overview**

Make it task-focused:

- index a repo
- ask code questions
- trace infrastructure
- inspect impact
- connect an assistant

- [ ] **Step 2: Move beginner CLI material out of `cli-reference.md`**

Keep `cli-reference.md` as lookup material. Move workflow prose into `use/*`
pages.

- [ ] **Step 3: Add MCP overview**

Create `mcp/index.md` with the plain-language difference between local MCP,
Compose MCP service, and deployed MCP/API usage.

- [ ] **Step 4: Verify**

Run MkDocs strict build, `git diff --check`, and a link-oriented `rg` scan for
old page names if any files were moved.

---

## Chunk 5: Split operations, concepts, extension, and reference

**Files:**

- Create: `docs/docs/operate/index.md`
- Create: `docs/docs/operate/health-checks.md`
- Create: `docs/docs/operate/telemetry.md`
- Create: `docs/docs/operate/troubleshooting.md`
- Create: `docs/docs/understand/index.md`
- Create: `docs/docs/extend/index.md`
- Modify or split: `docs/docs/reference/local-testing.md`
- Modify: `docs/docs/architecture.md`
- Modify: `docs/docs/concepts/*.md`
- Modify: `docs/docs/reference/telemetry/*.md`

- [ ] **Step 1: Split `local-testing.md`**

Move local user workflows into `run-locally/*`. Move operator validation into
`operate/*`. Keep only test-gate reference material in `reference/local-testing.md`.

- [ ] **Step 2: Build `Operate Eshu`**

Collect health checks, telemetry entry points, troubleshooting, and validation
runbooks.

- [ ] **Step 3: Build `Understand Eshu`**

Link architecture, graph model, modes, truth labels, and service workflows from
one concept page.

- [ ] **Step 4: Build `Extend Eshu`**

Link collector authoring, fact contracts, language support, plugin trust, and
source layout.

- [ ] **Step 5: Verify**

Run MkDocs strict build and `git diff --check`.

---

## Chunk 6: Humanize and final consistency pass

**Files:**

- Modify: all new overview and guide pages
- Modify: `README.md`
- Modify: `docs/docs/index.md`
- Modify: `docs/docs/start-here.md`

- [ ] **Step 1: Apply humanizer pass**

For each entry or guide page, scan for:

- inflated claims
- "not only / but also" phrasing
- decorative `-ing` clauses
- over-bolded labels
- generic endings
- repeated sentence shapes
- pages that explain before they orient

- [ ] **Step 2: Keep reference terse**

Do not make API, CLI, env var, or capability reference pages chatty. Remove
filler, but keep precise command and contract material.

- [ ] **Step 3: Run stale-term scan**

Run:

```bash
rg 'FalkorDB|falkordb|KuzuDB|kuzudb|retired NornicDB compose override|retired compose template|Neo4j is the default|external Neo4j|default Neo4j|Until then, Neo4j remains' \
  -n README.md docs/docs deploy/helm/eshu go/cmd go/internal docker-compose*.yml
```

Expected: no hits except intentional negative tests or clearly historical ADR
context.

- [ ] **Step 4: Run final verification**

Run:

```bash
helm lint deploy/helm/eshu
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
git diff --check
```

Expected: all exit 0.

- [ ] **Step 5: Final review checklist**

Confirm:

- the home page has three obvious paths
- local binaries and Docker Compose are separate
- Kubernetes deployment is first-class
- MCP is visible as its own path
- reference is narrower than before
- no generated `docs/site` files were hand-edited
- command blocks match code-validated behavior
