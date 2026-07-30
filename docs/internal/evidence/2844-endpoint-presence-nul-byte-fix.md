# Endpoint-Presence uid NUL-Byte Fix Evidence (#2844)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Endpoint-presence uid NUL-byte fix (#2844)

`apiEndpointRepoPathPresenceKey` (`endpoint_repo_path_presence.go`) synthesizes
the `(repo_id, path)` presence uid the #2809 handles_route/runs_in readiness gate
keys on. It previously joined `repo_id + "\x00" + path` and wrote the result to
the Postgres text `graph_endpoint_presence.uid` column. Postgres rejects NUL
bytes in text (`invalid byte sequence for encoding "UTF8": 0x00`, SQLSTATE
22021), so the presence upsert crashed and dead-lettered `workload_materialization`
for every endpoint-exposing repo, permanently starving the handles_route/runs_in
gate. The uid is now a SHA-256 hex digest of `repo_id + NUL + path`: the NUL stays
in the hash input (collision-free, separator-unambiguous) and never reaches
Postgres; the stored uid is hex and Postgres-safe.

No-Regression Evidence: `go test ./internal/reducer -run
'TestAPIEndpointRepoPathPresenceKeyShape|RepoPathPresence|FilterRowsByReadiness'
-count=1` — the regression test asserts the synthesized uid contains no NUL byte
and is deterministic, distinct across repos/paths, and separator-unambiguous; it
fails on the prior raw-join key and passes on the hashed key. The change swaps a
string concat for one SHA-256 over two short strings on the flag-gated presence
publish path (a no-op when the writer is nil), so it adds no measurable cost to
the endpoint commit path and no new graph write or query.

Observability Evidence: an operator sees the fix as `workload_materialization`
intents reaching `succeeded` instead of `dead_letter` with the
`record api endpoint repo/path presence: ... SQLSTATE 22021` error, the
`graph_endpoint_presence` table gaining rows for endpoint-exposing repos, and the
`handles_route`/`runs_in` shared-projection intents completing instead of looping
on "skipped intents until semantic readiness is committed". No new metric
instrument, label, span, or log line is added.
