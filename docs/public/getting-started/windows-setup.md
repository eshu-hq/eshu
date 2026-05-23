# Windows Setup

Windows users have two practical paths.

## Recommended: WSL

Use WSL for the closest Linux/macOS workflow:

1. Install WSL with Ubuntu.
2. Install Go inside WSL.
3. Build Eshu from the checkout.
4. Index from the WSL-visible project path.

Windows drives are available under `/mnt`, for example:

```bash
cd /mnt/c/Users/<you>/src/project
```

## Alternative: Native Windows With Neo4j

Native Windows should use an external Neo4j backend.

1. Install Go.
2. Build the CLI.
3. Start Neo4j and confirm Bolt is reachable.
4. Run `eshu neo4j setup`.
5. Verify with `eshu doctor`.

Build example:

```powershell
cd go
go build -o ..\eshu.exe .\cmd\eshu
cd ..
.\eshu.exe --help
```

## First Commands

```powershell
eshu doctor
eshu index .
eshu mcp setup
```

If `eshu` is not found, add the directory containing `eshu.exe` to `PATH` or run
the binary by full path.

## Verify

```powershell
eshu list
```
