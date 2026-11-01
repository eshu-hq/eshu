# Arbiter ruling — eshu-mcp #50 live registry-token probe is deferred to #54

RULING: eshu-hq/eshu-mcp#50 closes without the live subject-less file-registry
token probe. That probe is tracked in eshu-hq/eshu-mcp#54 and waits, because it
needs a live core with a registry file that no stack inside the closing goal
has. The tuning of the admission cache and probe values from real traffic is an
owner note, not an acceptance criterion, and gets no issue.

Arbiter: arbiter-eshu (Fable 5.1), 2026-11-01. Every claim below was re-read by
the arbiter from `gh`, the eshu-mcp and eshu checkouts, and CI logs at ruling
time.

## Why it waits

1. The probe is `GET /api/v0/capabilities` through the satellite with a
   file-registry token that carries no subject. The E2E compose stack mints no
   registry file: [`compose/docker-compose.e2e.yaml` in eshu-deploy at
   1e306ec5](https://github.com/eshu-hq/eshu-deploy/blob/1e306ec52529732aa3f21029bbb47fd508e44311/compose/docker-compose.e2e.yaml)
   (the pin the E2E checks out since eshu-hq/eshu#9758) sets no
   `ESHU_SCOPED_TOKENS_FILE` on any service, and neither does the base compose it
   extends, so no CI stack can run it.
2. Deploying the current satellite into a real environment is a separate
   change in the deploy repo and an operations act. It is not part of the
   closing goal and is not required by any #50 criterion. What the deployed
   environment pins and which credential its callers use is read from the
   deploy repo, not from this tree, and is listed under "What stays unverified".
3. What is proven today: the minimally scoped half ran live ([eshu run
   41282252178](https://github.com/eshu-hq/eshu/actions/runs/41282252178),
   job 124776524453, `shapeA_mcp_tool_call_row_filtered`: 171 tools listed to a scoped
   personal token with an empty grant, and `tools/list` is served only after the
   core probe admits); and both credential classes are pinned at the contract
   level by [eshu#9756](https://github.com/eshu-hq/eshu/pull/9756) (merged d92031f4) against the real `wrapAPIAuth`, `scopedtoken.ChainResolvers`
   and capability handler, with the identity SQL stubbed. The contract test
   satisfies the owner's 2026-11-01 02:50 request for a permanent core guard. It
   does not replace the live run, and the SQL text itself needs a live Postgres.

## What discharges #54

`GET /api/v0/capabilities` returns 200 through the satellite for a file-registry
token without a subject, on a live core, with the run id or command output posted
on [eshu-hq/eshu-mcp#54](https://github.com/eshu-hq/eshu-mcp/issues/54). Pointers: eshu#9756 and
eshu run 41282252178 above.

## What stays unverified

The deployed environment's satellite pin (an older release, by digest) and that
its callers use the shared key were read from the deploy repo during the ruling
and are not checkable from this tree.
`make ci` on eshu-mcp was not run by the arbiter; the CI run was read instead.
`gh attestation verify` was not run from the arbiter's host.
