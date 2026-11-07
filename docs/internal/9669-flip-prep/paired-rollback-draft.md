<!-- SPDX-License-Identifier: MIT -->
<!-- Copyright (c) 2025-2026 eshu-hq -->

# Paired rollback draft (#9669, arbiter §5 prep lane)

Refs #9669, part of #6707. Status: preparation only — documented, NOT
executed. Rollback here follows the migration design's producer-rollback
rule (`docs/internal/design/eshu-ecosystem-repository-migration.md:251-264`):
stop new claims, drain in-flight work, revoke authority, restore the last
supported version, replay from the last committed generation, verify exact
truth. At no point may two producers hold authority for the same scope and
generation.

A fresh arbiter review is required before any real flip, and therefore
before any real rollback.

## Shape 1 — edge-restore (consumers re-pinned to a bad dest artifact)

Use when the flip pin itself is bad: either edge serves wrong truth from a
dest image. Restore authority to the last-good core build.

```bash
# 0. Identify what moved (read-only)
kubectl -n eshu get deploy/eshu-api -o jsonpath='{.spec.template.spec.containers[*].image}{"\n"}'
kubectl -n eshu get deploy/eshu-mcp-server -o jsonpath='{.spec.template.spec.containers[*].image}{"\n"}'
gh release list --repo eshu-hq/eshu-mcp
```

```bash
# 1. Revert the cutover commit touching the eshu-deploy image values
#    (the per-service pins — verify names/paths by rendering at
#    rollback time; the shared image.repository/image.tag pair does
#    NOT move the dest edges). ArgoCD self-heals from git.
#    If git is unavailable, force values directly (chart at helm/eshu):
helm upgrade eshu helm/eshu \
  --set extractedApi.image.repository=TBD-LAST-GOOD-REPO \
  --set extractedApi.image.tag=TBD-LAST-GOOD-TAG \
  --set mcpServer.image=TBD-LAST-GOOD-MCP-IMAGE -n eshu
kubectl -n eshu rollout status deploy/eshu-extracted-api
kubectl -n eshu rollout status deploy/eshu-mcp-server
```

```bash
# 2. Verify (same probes as the parity plan, expect pre-flip results):
curl -sS -o /dev/null -w 'GET /sse: %{http_code}\n' http://TBD-MCP-HOST/sse
curl -sS -o /dev/null -w 'POST /mcp/message: %{http_code}\n' -X POST http://TBD-MCP-HOST/mcp/message \
  -H 'Content-Type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}'
# expect HTTP 401 unauthenticated on both; then one authenticated tools/list
# + tools/call smoke test, and the HTTP route census re-proof.
```

Durable-state note: no state moves in this boundary — Postgres/Neo4j stay
core-owned (migration design §State and communication boundaries). Restore
needs no data migration, fencing, or replay, UNLESS a future flip moves
claim/lease state — then fence the dest writer first (NOT_CHECKED until
such a design exists).

## Shape 2 — satellite-to-satellite (bad merge in a dest repo, cluster healthy)

Use when the cluster still serves the previous dest release correctly but a
dest repo took a bad merge (or a newer dest release is bad and the older
one is good). No cluster authority moves; fix the repo.

```bash
# eshu-mcp and/or eshu-api destination checkouts:
git -C TBD-DEST-CHECKOUT revert TBD-BAD-MERGE-SHA && git push origin main
# then re-pin eshu-deploy to the previous good dest tag and let ArgoCD sync,
# or re-run the dest release chain (commit, reviewed PR, tag, image + cosign
# + attestations, checklist) per the eshu-release chain.
```

## Placeholders (explicitly TBD — fill at flip time, never invent now)

| Placeholder | Meaning | Fill with |
| --- | --- | --- |
| `TBD-LAST-GOOD-REPO` / `TBD-LAST-GOOD-TAG` | Last-supported artifact coordinates | Dest-or-core image repo + tag/digest from the flip pin record (D11 condition C4; the truncated year-old #8873 record is not usable) |
| `TBD-MCP-HOST` / API host | Flipped topology addresses | Live service hostnames/ports at flip time |
| `TBD-DEST-CHECKOUT` / `TBD-BAD-MERGE-SHA` | Bad dest change identity | Clone path + reverted SHA if Shape 2 fires |
| Evidence location `TBD` | Where rollback proof lands | PR/issue comment URL + command log with exit codes |
| Topology `TBD` | What serves what after restore | Deployment image digests + ArgoCD sync state post-restore |
| Artifact digest `TBD` | Exact restored bytes | `image@sha256:…` for both Deployments post-restore |

Flip pin record procedure (C4): the flip executor captures complete
`repo:tag@digest` coordinates for both Deployments pre-flip (core
last-good) and post-flip (dest) and writes them here and into the
flip record. Pin holder: named by the owner before the lane closes
(TBD — flagged on #9669, not filled here).

## Rollback execution log (empty until executed)

No rollback has been executed under this plan. When one is, append: date,
trigger, shape used, exact commands with exit codes, restored digests,
parity re-proof output, and owner acceptance. The #8873 rollback-path proof
(pre-cutover, read-only) is the pattern to follow, not a substitute.

## Open questions (D11 arbiter conditions in parentheses)

1. Who holds the flip pin record (last-good coordinates) at flip time?
   Owner names the holder before the lane closes (C4).
2. Does ArgoCD self-heal fast enough that the `helm upgrade` fallback is
   unnecessary — or must both paths be rehearsed?
3. Owner confirms the “no state moves” note still holds after the flip diff;
   reconfirm against the final flip diff immediately before execution —
   if any claim/lease state moves, Shape 1 is void until a fenced design
   lands (C7).
