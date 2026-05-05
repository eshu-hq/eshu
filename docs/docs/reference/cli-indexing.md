# CLI: Indexing & Management

These commands are the foundation of Eshu. They allow you to add, remove, and monitor the code repositories in your graph.

## `eshu index`

Adds a code repository to the graph database. This is the first step for any project.

For directory and workspace targets, this command launches the
`bootstrap-index` runtime in direct filesystem mode.

!!! info "Excluding Files (.eshuignore)"
    Eshu already skips hidden and well-known cache directories such as `.git`, `.terraform`, `.terragrunt-cache`, `.pulumi`, `.crossplane`, `.serverless`, `.aws-sam`, and `cdk.out`.
    It also excludes built-in dependency roots such as `vendor/`, `node_modules/`, `site-packages/`, and `deps/` before parse by default.
    Use `.eshuignore` for project-specific exclusions beyond those built-in defaults.
    **[📄 Read the .eshuignore Guide](eshuignore.md)**

**Usage:**
```bash
eshu index [path] [options]
```

**Common Options:**

*   `path`: The folder to index (default: current directory).
*   `--force`: Re-index from scratch, even if it looks unchanged.
*   `--discovery-report <file>`: Write a JSON discovery advisory report for
    noisy-repo tuning. The report lists discovered, parsed, skipped, and
    materialized file/entity counts plus top noisy directories/files and skip
    breakdowns. The JSON includes `schema_version=discovery_advisory.v1` so
    local scripts can fail closed if the advisory shape changes. It is an
    operator artifact, not a high-cardinality metric.
    For the full evidence → config → rerun workflow, see
    [Local Testing — Discovery Advisory Playbook](local-testing.md#discovery-advisory-playbook).

**Runtime Notes:**

*   Local index state for the Go launcher is stored under `ESHU_HOME/state/go-bootstrap-index/`.
*   `--discovery-report` forwards `ESHU_DISCOVERY_REPORT=<absolute path>` to
    `bootstrap-index`, which writes one JSON array containing an advisory per
    collected repository.
*   When using the local Eshu service (`eshu watch`, `eshu mcp stdio`),
    per-workspace state lives under
    `${ESHU_HOME}/local/workspaces/<workspace_id>/`. Workspace-root resolution
    order and data-root layout are documented in
    [CLI Reference — Workspace root and profiles](cli-reference.md#workspace-root-and-profiles)
    and [Local Data Root Spec](local-data-root-spec.md).
*   The command still honors `.gitignore`, `.eshuignore`, and the configured parse-worker settings.

**Example:**
```bash
# Index the current folder
$ eshu index .

# Index a specific project
$ eshu index /home/user/projects/backend-api
```

---

## `eshu list`

Shows all repositories currently stored in your graph database.

**Usage:**
```bash
eshu list
```

**Example Output:**
```text
Indexed Repositories:
1. /home/user/projects/backend-api (Nodes: 1205)
2. /home/user/projects/frontend-ui (Nodes: 850)
```

---

## `eshu watch`

Starts a real-time monitor. If you edit a file, the graph updates instantly.

The watch path runs end to end through the current local refresh flow:

- when the watched repo or workspace is missing index state, the initial scan
  launches the Go `bootstrap-index` runtime
- after startup, filesystem events are debounced into repo-level reindex runs
  through the same indexing path

!!! warning "Foreground Process"
    This command runs in the foreground. Open a new terminal tab to keep it running.

**Usage:**
```bash
eshu watch [path]
```

**Example:**
```bash
$ eshu watch .
[INFO] Watching /home/user/projects/backend-api for changes...
[INFO] Detected change in users/models.py. Re-indexing...
```

This is the CLI-friendly local equivalent of the long-running sync and re-index loop used in the deployable-service runtime.

For multi-repository local indexing, use `eshu workspace index`. The public Go
CLI keeps ecosystem-wide indexing on the `workspace` and admin flows rather
than on separate ecosystem indexing commands.

---

## Compatibility Stubs

The current Go CLI still carries a few compatibility stubs so older operator
muscle memory gets a directed error instead of a silent behavior change:

- `eshu delete`
- `eshu clean`
- `eshu add-package`
- `eshu ecosystem index`
- `eshu ecosystem status`

Deletion, cleanup, and recovery are owned by the Go admin/runtime surfaces
rather than ad hoc local CLI mutations.

---

## Related docs

- [Troubleshooting](troubleshooting.md)
