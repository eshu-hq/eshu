# Windows Setup

Windows users have two practical paths:

## Recommended: WSL

Use WSL when you want the smoothest local development workflow.

Typical flow:

1. install WSL
2. open an Ubuntu shell
3. install Go
4. build the Eshu CLI from source
5. index from the WSL-visible project path

This keeps Eshu close to the Linux/macOS experience and avoids backend limitations on native Windows.

## Alternative: native Windows + Neo4j

If you do not want WSL, use **Neo4j** as the backend and run Eshu against that external database.

Recommended checklist:

1. install Go
2. build the Eshu CLI from source
3. ensure Neo4j is reachable
4. run `eshu neo4j setup`
5. verify with `eshu doctor`

Build example:

```powershell
cd go
go build -o ..\\eshu.exe .\\cmd\\eshu
cd ..
.\eshu.exe --help
```

## First commands

```powershell
eshu doctor
eshu index .
eshu mcp setup
```

If path or shell behavior differs in your environment, put the built binary on
your PATH or invoke it directly with its full path.

---

## Common Windows Issues

### `'eshu' command not found`

* Restart your terminal after building the CLI
* Ensure the directory containing `eshu.exe` is in PATH
* Or run the binary directly from the checkout path

### WSL path confusion

Windows drives can be accessed from WSL using:

```bash
cd /mnt/c/
```

### Neo4j connection issues

* Ensure Neo4j is running
* Check URI format: `bolt://localhost:7687`
* Double-check username/password

---

## Verify Installation

Run:

```bash
eshu list
```
If no errors are shown, setup is complete.
