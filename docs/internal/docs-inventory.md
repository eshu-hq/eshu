# Docs Inventory

This inventory describes the documentation surfaces that are tracked in the
current repository state. It is intentionally current-state only.

## Public docs

The public site is built from:

- `docs/mkdocs.yml`
- `docs/docs/`

The public docs are grouped into these active families:

| Area | Paths | Purpose |
| --- | --- | --- |
| Landing and product narrative | `docs/docs/index.md`, `architecture.md`, `why-eshu.md`, `use-cases.md` | Explain what Eshu is, why it exists, and how the platform fits together. |
| Concepts | `docs/docs/concepts/*.md` | Core mental models such as graph semantics, processing flow, and runtime modes. |
| Getting started | `docs/docs/getting-started/*.md` | Installation, prerequisites, quickstart, and workstation setup. |
| Deployment | `docs/docs/deployment/*.md` | Docker Compose, Helm, Argo CD, manifests, service runtimes, and deployment boundaries. |
| Guides | `docs/docs/guides/*.md` | Workflow guides, visualization, relationship examples, fixture ecosystems, CI/CD, and Terraform provider operations. |
| Services | `docs/docs/services/*.md` | Service-level operational pages for `bootstrap-index`, `ingester`, and `resolution-engine`. |
| Reference | `docs/docs/reference/*.md` and `docs/docs/reference/telemetry/*.md` | CLI, HTTP API, runtime admin API, local testing, logging, telemetry, workflows, parity, relationship mapping, and troubleshooting. |
| Language support | `docs/docs/languages/*.md` | Parser behavior pages plus feature and support matrices for supported languages and IaC families. |
| Supporting assets | `docs/docs/images/*`, `docs/openapi/runtime-admin-v1.yaml`, `docs/diagrams/*.mmd`, `docs/dashboards/*.json` | Images, diagrams, OpenAPI, and dashboard assets used by the public docs and operator workflows. |

## Internal docs

Maintainer-only docs live under `docs/internal/`.

| Path | Purpose | Status |
| --- | --- | --- |
| `docs/internal/README.md` | Boundary rules for internal versus public docs | active |
| `docs/internal/docs-inventory.md` | This inventory | active |
| `docs/internal/updating-docs.md` | Maintainer workflow for updating docs | active |

Untracked investigation notes may exist in `docs/internal/` during active work,
but they are not part of the stable docs set until they are intentionally
committed.

## Retired historical material

- There is no tracked `docs/superpowers/` documentation tree in the current
  worktree.
- There is no tracked `docs/archive/` tree in the current worktree.
- Historical plans and migration records should stay out of the public docs
  unless they are rewritten into current architecture, workflow, or operator
  guidance.
