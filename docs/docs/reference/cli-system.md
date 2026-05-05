# CLI: System & Configuration

Commands to manage the Eshu engine itself.

## `eshu doctor`

Self-diagnostic tool. Runs a health check on your installation.

**Checks performed:**

*   Config directory and `.env` presence.
*   Go runtime binaries on `PATH` (`eshu-api`, `eshu-mcp-server`,
    `eshu-bootstrap-index`, `eshu-ingester`, `eshu-reducer`).
*   API health at the configured local base URL.
*   Neo4j URI configuration.
*   Postgres DSN configuration.

**Usage:**
```bash
eshu doctor
```

---

## `eshu mcp setup`

The interactive wizard for configuring AI clients.

**What it does:**

1.  Detects installed AI Clients (Cursor, VS Code, Claude).
2.  Creates the necessary config files (e.g., `mcp.json`).
3.  Generates a `.env` file with database credentials.

**Usage:**
```bash
eshu mcp setup
```

---

## `eshu neo4j setup`

The interactive wizard for configuring the graph database backend.

**What it does:**

*   **Docker:** Pulls and runs the official Neo4j image.
*   **Local:** Helps locate a local installation.
*   **Remote:** Configures credentials for AuraDB.

**Usage:**
```bash
eshu neo4j setup
```

---

## `eshu config` Commands

Directly modify settings without editing text files.

*   `eshu config show`: Print current configuration.
*   `eshu config set <key> <value>`: Update a setting.
    *   Example: `eshu config set ESHU_GRAPH_BACKEND nornicdb`
*   `eshu config db <backend>`: Switch backends (shortcut).
    *   Example: `eshu config db neo4j`
