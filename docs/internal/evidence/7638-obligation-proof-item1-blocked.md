# Item-1 built-binary drive: BLOCKED on container runtime (Refs #7638)

Status: **BLOCKED — live drive not run.** No production code changed in this
step. No `make pre-push`, no push, no PR.

## Time box

90-minute wall-clock box stated at task start for the drive setup, both arms,
and the single-repo arm. The container-runtime probe failed at ~minute 10, so
the box was stopped early and pivoted to fix-and-document per the task brief:
record the blocker, settle the NOT_CHECKED re-ingest question from source, and
leave an exact resume runbook. No code fix was warranted (evidence-only step,
infra blocker, nothing to TDD against).

## Blocker: exact command and cause

```bash
docker info
# => Server: failed to connect to the docker API at
#    unix:///var/run/docker.sock: dial unix /var/run/docker.sock:
#    connect: no such file or directory (exit 1)
```

- `/var/run/docker.sock` is a dangling symlink to
  `~/.orbstack/run/docker.sock`, which does not exist.
- The `desktop-linux` (`~/.docker/run/docker.sock`) socket is also absent.
- `OrbStack.app` exists in `/Applications` but its daemon is not running and
  there is no `orbstack` CLI on PATH, so the runtime cannot be revived from
  this session's scope. Docker Compose v5.5.0 client is present but has no
  server to talk to.
- Worktree/branch (unchanged, clean): `eshu-wt/7638-obligation-proof` (under the Eshu worktrees root),
  branch `feat/7638-obligation-proof-p1`, base `origin/main e7675d9ee`.

Nothing was faked: zero drive assertions were executed, so there are no
per-assertion results, no control-arm result, and no single-repo result to
report. All three are NOT_RUN (blocked), not RED or GREEN.

## Settled from source: compose ingester re-ingests a changed local repo WITHOUT restart

The issue records this as NOT_CHECKED pending `docs/public/run-locally/docker-compose.md`.
The doc does not answer it; the source does. Answer: **yes**, for any change
that moves the tree fingerprint, on the next collector poll, no restart needed.

- `syncFilesystemRepositories` (`go/internal/collector/repo/git/selection_filesystem.go`)
  runs on every collector cycle (the `fingerprintTree` comment: "runs on every
  poll"). It fingerprints the whole `FilesystemRoot` and compares against the
  stored manifest (`<repos-dir>/.eshu-fixture-manifest`); a differing
  fingerprint re-materializes the corpus and returns `CorpusChanged: true`,
  while an unchanged tree returns an empty selection (skip, no re-ingest).
- `fingerprintTree` hashes sha256 over `(relative path, mtime-nanos, size)` per
  regular file. A normal manifest edit changes mtime (and usually size), so it
  is picked up. Edge cases that would NOT trigger: an edit preserving both size
  and mtime-nanos (e.g. a crafted `touch -r` + same-length rewrite), and
  symlink-only changes (symlinks are skipped by the walk).
- Scheduled sync is on by default in this path: `parseScheduledSyncEnabled`
  (`go/internal/collector/scheduled_sync_config.go`) returns true on empty, and
  the collector poll interval is 1s (`ingesterCollectorPollInterval` in
  `go/cmd/ingester/wiring.go`). `docker-compose.neo4j.yml` does not set
  `ESHU_REPO_SCHEDULED_SYNC_ENABLED` at all (only `docker-compose.yaml:412`
  does), so the code default governs — do not "fix" this by adding the var with
  a different value.
- The `:ro` fixtures mount is not an obstacle: the edit happens on the host
  directory referenced by `ESHU_FILESYSTEM_HOST_ROOT`; the container sees it
  through the bind mount on the next poll.

## Compose gaps the resume run MUST handle (read from `docker-compose.neo4j.yml` at `e7675d9ee`)

1. The `resolution-engine` service has **no** `ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED`
   passthrough (verified: zero matches in both compose files). Exporting the var
   in the shell alone does nothing. The flag-ON arm needs an overlay file, e.g.
   `docker-compose.7638-obligation.yml`, adding to `resolution-engine.environment`:
   `ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED: "true"`. The flag's registry row
   (`go/internal/envregistry/entries.go`, default `false`, subsystem `reducer`)
   and wiring (`go/cmd/reducer/activation_obligation_wiring.go`, off → runner nil)
   confirm reader/writer sides.
2. The default `ESHU_FILESYSTEM_HOST_ROOT` (`./tests/fixtures/ecosystems`) is not
   the two-repo shape item 1 needs. Point it at a prepared host dir: repo A
   (source, deployment manifest referencing repo B) + repo B (target).
3. The single-repo arm (item 9) needs a one-repo root whose quiet generation is
   the only active partition. RED expectation with the flag on is
   hold-until-later-commit (`no_memo_baseline` refusal at lease cadence) —
   record, do not fix here.

## Resume runbook (not executed)

```bash
cd path/to/eshu-wt/7638-obligation-proof
# 0. Revive runtime first: start OrbStack/Docker Desktop until `docker info` exits 0.
# 1. Prepare host fixture roots: /tmp/7638-two-repo/{source,target}, /tmp/7638-single-repo/{only}.
# 2. Flag-ON arm:
ESHU_FILESYSTEM_HOST_ROOT=/tmp/7638-two-repo \
docker compose -f docker-compose.neo4j.yml -f docker-compose.7638-obligation.yml up --build -d \
  ingester resolution-engine projector eshu mcp-server
# 3. Converge to terminal queue, then edit ONLY the source manifest on the host.
# 4. Within one lease assert: obligation row completed; exact-generation
#    backward_evidence_committed phase row; deployment_mapping woken+succeeded;
#    DEPLOYS edge via Cypher (bolt :7687) AND HTTP API (:8080) AND MCP tool (:8081).
# 5. Control arm: same drive with the overlay removed (flag off) — expect no phase
#    until a later commit.
# 6. Single-repo arm with flag on — expect hold (RED per issue, record only).
```
